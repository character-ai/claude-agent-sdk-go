# Bug Analysis Report - Claude Agent SDK Go

This document details bugs found in the codebase during comprehensive analysis.

## Critical Bugs (Security/Correctness)

### 1. Race Condition in `client.Close()` - CRITICAL

**File:** `client.go:472-497`

**Issue:** Potential panic due to channel being closed between read and wait.

```go
func (c *Client) Close() error {
    c.mu.Lock()
    if !c.running || c.cmd == nil || c.cmd.Process == nil {
        c.mu.Unlock()
        return nil
    }
    proc := c.cmd.Process
    done := c.done    // Reading c.done
    c.mu.Unlock()     // Lock released

    // Send SIGINT for graceful shutdown
    if err := proc.Signal(syscall.SIGINT); err != nil {
        return nil
    }

    // RACE: Between unlock and here, streamEvents could finish and close c.done
    select {
    case <-done:      // Panic possible if closed between line 479 and here
        return nil
    case <-time.After(5 * time.Second):
        return proc.Kill()
    }
}
```

**Impact:** Panic if `streamEvents()` completes and closes `done` channel between lines 479-491.

**Fix:** Copy `done` channel reference while holding lock, or use atomic operations.

### 2. Process Leak in `client.runStreaming()` - CRITICAL

**File:** `client.go:239-256`

**Issue:** If `cmd.Start()` succeeds but function returns error early, process leaks.

```go
func (c *Client) runStreaming(ctx context.Context, args []string) (<-chan Event, error) {
    // ... get pipes ...

    if err := cmd.Start(); err != nil {
        return nil, fmt.Errorf("failed to start claude CLI: %w", err)
    }
    // Process is now running

    c.cmd = cmd
    c.stdin = stdin
    // ... more assignments ...

    events := make(chan Event, 100)
    go c.streamEvents(ctx, stdout, stderr, events, cmd)

    return events, nil
    // If we returned error after cmd.Start(), process would leak
}
```

**Impact:** Process left running with no cleanup mechanism.

**Fix:** Add deferred cleanup or ensure goroutine is started before any return path.

### 3. Hook Goroutine Leak Under Timeout - HIGH

**File:** `hooks.go:326-338`

**Issue:** When hook times out, goroutine continues running after timeout.

```go
if sh.Timeout > 0 {
    timeoutCtx, cancel := context.WithTimeout(ctx, sh.Timeout)
    done := make(chan HookResult, 1)
    go func() {
        done <- hook(timeoutCtx, hookCtx)  // Goroutine continues even after timeout
    }()
    select {
    case hookResult = <-done:
    case <-timeoutCtx.Done():
        cancel()
        return DenyHook("hook timed out"), nil  // Returns but goroutine still running
    }
    cancel()
}
```

**Impact:** Goroutine leaks if hook execution exceeds timeout, consuming resources.

**Fix:** Hook implementations must respect context cancellation.

## High Priority Bugs (Reliability)

### 4. Silently Ignored Errors in Hook Registration - HIGH

**File:** `hooks.go:170-244`

**Issue:** All `h.store.InsertHook()` calls ignore errors.

```go
func (h *Hooks) addPreHookInternal(matcher string, isRegex bool, timeout time.Duration, hook PreToolUseHook) {
    // ... logic ...
    _ = h.store.InsertHook(updated)  // Error silently ignored
    return
}
```

**Impact:** Hook registration failures are silently lost. No indication that hook wasn't registered.

**Fix:** Return error or log failure.

### 5. Silently Ignored Errors in Tool Operations - HIGH

**File:** `tools.go:86-95, 140-152`

**Issue:** Store errors are ignored and `nil` is returned.

```go
func (r *ToolRegistry) Definitions() []ToolDefinition {
    tools, err := r.store.ListTools()
    if err != nil {
        return nil  // Returning nil instead of empty slice
    }
    // ... build defs ...
}
```

**Impact:** Callers cannot distinguish between "no tools" and "error retrieving tools". Nil slice vs empty slice have different behaviors.

**Fix:** Return empty slice on error or change signature to return error.

### 6. Stderr Goroutine May Not Exit - MEDIUM-HIGH

**File:** `client.go:285-290`

**Issue:** Goroutine created to read stderr with `io.ReadAll` but if context is cancelled mid-execution, this goroutine may not exit properly.

```go
stderrCh := make(chan string, 1)
go func() {
    defer close(stderrCh)
    data, _ := io.ReadAll(stderr)  // May block if context cancelled
    stderrCh <- string(data)
}()
```

**Impact:** Goroutine may block on ReadAll if process is killed abruptly.

**Fix:** Consider using io.Copy with context-aware reader or ensure stderr is closed.

### 7. Stream Error Sends Partial Events - MEDIUM-HIGH

**File:** `api_agent.go:423`

**Issue:** In `streamTurn()`, `stream.Err()` is checked but previous events already sent to channel.

```go
if err := stream.Err(); err != nil {
    return nil, nil, apiTurnUsage{}, fmt.Errorf("stream error: %w", err)
}
```

**Impact:** Partial events are already emitted before error detected. Inconsistent state where some events processed but response incomplete.

**Fix:** Add error event or document partial event behavior.

## Medium Priority Bugs

### 8. Off-by-One Error in Budget Checking - MEDIUM

**File:** `budget.go:69`

**Issue:** Check is strictly greater than instead of greater-than-or-equal.

```go
if b.cfg.MaxTokens > 0 && b.tokens > b.cfg.MaxTokens {
    return &BudgetExceededError{...}
}
```

**Impact:** Can use exactly `MaxTokens + 1` tokens before failing. Should be `>=` to enforce strict limit.

**Example:** If MaxTokens=100, will allow up to 101 tokens before error.

**Fix:** Change to `b.tokens >= b.cfg.MaxTokens`.

### 9. Regex Compilation Race - MEDIUM (Performance)

**File:** `hooks.go:288-304`

**Issue:** Double-checked locking pattern allows duplicate regex compilations.

```go
func (h *Hooks) getCompiledRegex(pattern string) *regexp.Regexp {
    h.regexMu.RLock()
    re, ok := h.regexes[pattern]
    h.regexMu.RUnlock()
    if ok {
        return re
    }

    compiled, err := regexp.Compile(pattern)  // Between RUnlock and Lock,
    if err != nil {                            // another thread could compile same regex
        return nil
    }
    h.regexMu.Lock()
    h.regexes[pattern] = compiled  // Duplicate compilations possible
    h.regexMu.Unlock()
    return compiled
}
```

**Impact:** Multiple threads could compile same regex (performance issue, not correctness).

**Fix:** Check map again after acquiring write lock, or use sync.Once per pattern.

### 10. Silent Content Block Parse Failures - MEDIUM

**File:** `types.go:307-335`

**Issue:** `parseContentBlocks()` silently skips parse errors.

```go
func parseContentBlocks(rawBlocks []json.RawMessage) []ContentBlock {
    blocks := make([]ContentBlock, 0, len(rawBlocks))
    for _, raw := range rawBlocks {
        var meta struct {
            Type ContentBlockType `json:"type"`
        }
        if err := json.Unmarshal(raw, &meta); err != nil {
            continue  // Silently skipped
        }
        switch meta.Type {
        case ContentTypeText:
            var tb TextBlock
            if err := json.Unmarshal(raw, &tb); err == nil {  // Error ignored
                blocks = append(blocks, tb)
            }
            // No default case - unknown types silently ignored
        }
    }
    return blocks
}
```

**Impact:** Silent data loss if content block format unexpected.

**Fix:** Log warnings or return error slice.

### 11. Lost Metrics on Concurrent Updates - MEDIUM

**File:** `metrics.go:137-158`

**Issue:** In `recordToolEnd()`, `toolStartTimes` is deleted without confirming tool was recorded.

```go
func (m *MetricsCollector) recordToolEnd(name, id string, isError bool) {
    // ... lock acquired ...
    delete(m.toolStartTimes, id)  // Deleted even if recordToolStart never called
    // ...
}
```

**Impact:** If `recordToolEnd()` called without matching `recordToolStart()`, silently ignored. Metrics inconsistency in concurrent scenarios.

**Fix:** Check if key exists before deleting, or log warning.

### 12. Snapshot Not Properly Released - MEDIUM

**File:** `store.go:374-387`

**Issue:** `StoreSnapshot.Close()` calls `txn.Abort()` but doesn't verify success.

```go
type StoreSnapshot struct {
    txn *memdb.Txn
}

func (ss *StoreSnapshot) Close() {
    ss.txn.Abort()  // No error check
}
```

**Impact:** If caller forgets to call Close(), read transaction leaks. Long-held snapshots can cause memdb memory pressure. No timeout mechanism for snapshots.

**Fix:** Document Close() requirement, consider finalizer, or add context deadline.

## Low Priority Issues

### 13. Context Not Propagated to Permission Callbacks - LOW

**File:** `agent.go:25-27`

**Issue:** `canUseTool` callback receives context but `PermissionDecision` can't indicate timeout/cancellation.

```go
type CanUseToolFunc func(ctx context.Context, toolName, toolID string, input json.RawMessage) PermissionDecision
```

**Impact:** If callback ignores context deadline, tool execution may proceed after context cancelled.

**Fix:** Add context awareness to PermissionDecision or document callback responsibility.

## Summary Statistics

- **Total Bugs Identified:** 13
- **Critical:** 3 (race conditions, resource leaks)
- **High:** 4 (silent errors, goroutine leaks)
- **Medium:** 5 (logic errors, performance issues)
- **Low:** 1 (API design)

## Recommended Priority Order for Fixes

1. Fix race condition in `client.Close()` (#1) - CRITICAL
2. Fix process leak in `client.runStreaming()` (#2) - CRITICAL
3. Fix silently ignored errors in hooks and tools (#4, #5) - HIGH
4. Fix off-by-one in budget checking (#8) - MEDIUM (easy fix)
5. Fix hook goroutine leak (#3) - HIGH (requires documentation)
6. Address stderr goroutine issue (#6) - MEDIUM-HIGH
7. Document stream error behavior (#7) - MEDIUM-HIGH
8. Remaining items as time permits

## Testing Recommendations

1. Add race detector tests: `go test -race ./...`
2. Add stress tests for concurrent Close() calls
3. Add tests for hook timeout behavior
4. Add tests for budget edge cases (exactly at limit)
5. Add tests for error handling paths
6. Add tests for resource cleanup

## Notes

- All critical bugs involve concurrency or resource management
- Many bugs are silent failures (errors ignored, data skipped)
- Good test coverage exists but needs expansion for edge cases
- Consider adding more comprehensive error handling throughout
