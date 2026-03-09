package claudeagent

import (
	"sync"
	"time"
)

// ToolStats accumulates statistics for a single tool across all invocations in a session.
type ToolStats struct {
	// Name is the tool name.
	Name string
	// Calls is the total number of invocations.
	Calls int
	// Failures is the number of invocations that returned an error.
	Failures int
	// TotalTime is the cumulative execution time across all calls.
	TotalTime time.Duration
}

// AvgTime returns the average execution time per call.
func (s *ToolStats) AvgTime() time.Duration {
	if s.Calls == 0 {
		return 0
	}
	return s.TotalTime / time.Duration(s.Calls)
}

// TurnMetrics captures timing and tool data for a single LLM turn.
type TurnMetrics struct {
	// TurnIndex is the zero-based index of this turn within the session.
	TurnIndex int
	// LLMLatency is the wall-clock time spent waiting for the LLM to respond.
	LLMLatency time.Duration
	// ToolsInvoked lists the names of tools called during this turn (in invocation order).
	ToolsInvoked []string
}

// LoopMetrics is a point-in-time snapshot of session-level metrics.
type LoopMetrics struct {
	// SessionDuration is the total elapsed time for the session.
	SessionDuration time.Duration
	// Turns holds per-turn metrics, indexed by turn order.
	Turns []TurnMetrics
	// ToolStats maps tool name to aggregated call statistics.
	ToolStats map[string]*ToolStats
}

// MetricsCollector gathers per-turn and per-tool metrics during agent execution.
// Configure it via AgentConfig.Metrics or APIAgentConfig.Metrics.
//
// Example:
//
//	mc := claudeagent.NewMetricsCollector()
//	agent := claudeagent.NewAgent(claudeagent.AgentConfig{
//	    Metrics: mc,
//	    // ...
//	})
//	// After run completes:
//	summary := mc.Snapshot()
type MetricsCollector struct {
	mu             sync.Mutex
	sessionStart   time.Time
	sessionEnd     time.Time
	turns          []TurnMetrics
	toolStats      map[string]*ToolStats
	toolStartTimes map[string]time.Time // keyed by toolUseID
}

// NewMetricsCollector creates a ready-to-use metrics collector.
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		toolStats:      make(map[string]*ToolStats),
		toolStartTimes: make(map[string]time.Time),
	}
}

// Snapshot returns a copy of the current accumulated metrics.
// Safe to call concurrently with ongoing agent execution.
func (m *MetricsCollector) Snapshot() LoopMetrics {
	m.mu.Lock()
	defer m.mu.Unlock()

	dur := m.sessionEnd.Sub(m.sessionStart)
	if m.sessionEnd.IsZero() && !m.sessionStart.IsZero() {
		dur = time.Since(m.sessionStart)
	}

	turns := make([]TurnMetrics, len(m.turns))
	for i, t := range m.turns {
		cp := TurnMetrics{
			TurnIndex:  t.TurnIndex,
			LLMLatency: t.LLMLatency,
		}
		if len(t.ToolsInvoked) > 0 {
			cp.ToolsInvoked = append([]string(nil), t.ToolsInvoked...)
		}
		turns[i] = cp
	}

	stats := make(map[string]*ToolStats, len(m.toolStats))
	for k, v := range m.toolStats {
		cp := *v
		stats[k] = &cp
	}

	return LoopMetrics{
		SessionDuration: dur,
		Turns:           turns,
		ToolStats:       stats,
	}
}

func (m *MetricsCollector) recordSessionStart() {
	m.mu.Lock()
	m.sessionStart = time.Now()
	m.mu.Unlock()
}

func (m *MetricsCollector) recordSessionEnd() {
	m.mu.Lock()
	m.sessionEnd = time.Now()
	m.mu.Unlock()
}

func (m *MetricsCollector) recordTurn(tm TurnMetrics) {
	m.mu.Lock()
	m.turns = append(m.turns, tm)
	m.mu.Unlock()
}

func (m *MetricsCollector) recordToolStart(toolUseID string) {
	m.mu.Lock()
	m.toolStartTimes[toolUseID] = time.Now()
	m.mu.Unlock()
}

func (m *MetricsCollector) recordToolEnd(toolName, toolUseID string, isError bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	start, ok := m.toolStartTimes[toolUseID]
	if !ok {
		return
	}
	elapsed := time.Since(start)
	delete(m.toolStartTimes, toolUseID)

	stats, exists := m.toolStats[toolName]
	if !exists {
		stats = &ToolStats{Name: toolName}
		m.toolStats[toolName] = stats
	}
	stats.Calls++
	stats.TotalTime += elapsed
	if isError {
		stats.Failures++
	}
}
