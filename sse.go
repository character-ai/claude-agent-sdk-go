package claudeagent

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// SSEWriter wraps an http.ResponseWriter for Server-Sent Events.
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewSSEWriter creates an SSE writer from an HTTP response writer.
func NewSSEWriter(w http.ResponseWriter) (*SSEWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("response writer does not support flushing")
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	return &SSEWriter{w: w, flusher: flusher}, nil
}

// WriteEvent writes an SSE event with the given type and data.
func (s *SSEWriter) WriteEvent(eventType string, data any) error {
	var dataStr string

	switch v := data.(type) {
	case string:
		dataStr = v
	case []byte:
		dataStr = string(v)
	default:
		jsonData, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("failed to marshal data: %w", err)
		}
		dataStr = string(jsonData)
	}

	_, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", eventType, dataStr)
	if err != nil {
		return err
	}

	s.flusher.Flush()
	return nil
}

// WriteAgentEvent writes an AgentEvent as an SSE event.
func (s *SSEWriter) WriteAgentEvent(event AgentEvent) error {
	eventData := map[string]any{
		"type": event.Type,
	}

	if event.Content != "" {
		eventData["content"] = event.Content
	}
	if event.ToolCall != nil {
		eventData["tool_call"] = event.ToolCall
	}
	if event.ToolResponse != nil {
		eventData["tool_response"] = event.ToolResponse
	}
	if event.Result != nil {
		eventData["result"] = event.Result
	}
	if event.Error != nil {
		eventData["error"] = event.Error.Error()
	}

	return s.WriteEvent(string(event.Type), eventData)
}

// Close sends a final close event.
func (s *SSEWriter) Close() error {
	return s.WriteEvent("close", map[string]string{"status": "complete"})
}

// StreamAgentToSSE connects an agent event channel to an SSE writer.
func StreamAgentToSSE(events <-chan AgentEvent, sse *SSEWriter) error {
	for event := range events {
		if err := sse.WriteAgentEvent(event); err != nil {
			return err
		}
	}
	return sse.Close()
}

// AgentHTTPHandler creates an HTTP handler that runs an agent and streams SSE.
func AgentHTTPHandler(agent *Agent) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		prompt := r.URL.Query().Get("prompt")
		if prompt == "" {
			http.Error(w, "prompt required", http.StatusBadRequest)
			return
		}

		sse, err := NewSSEWriter(w)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		events, err := agent.Run(r.Context(), prompt)
		if err != nil {
			sse.WriteEvent("error", map[string]string{"error": err.Error()})
			return
		}

		StreamAgentToSSE(events, sse)
	}
}
