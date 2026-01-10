package claudeagent

import (
	"context"
)

// Query sends a prompt to Claude and returns a channel of streaming events.
// This is a convenience function that creates a temporary client.
func Query(ctx context.Context, prompt string, opts ...Options) (<-chan Event, error) {
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	} else {
		o = DefaultOptions()
	}

	client := NewClient(o)
	return client.Query(ctx, prompt)
}

// QuerySync sends a prompt and collects all text responses into a single string.
// This blocks until the query completes.
func QuerySync(ctx context.Context, prompt string, opts ...Options) (string, *ResultMessage, error) {
	events, err := Query(ctx, prompt, opts...)
	if err != nil {
		return "", nil, err
	}

	var text string
	var result *ResultMessage

	for event := range events {
		if event.Error != nil {
			return text, result, event.Error
		}

		// Accumulate text from deltas
		if event.Text != "" {
			text += event.Text
		}

		// Capture the final result
		if event.Result != nil {
			result = event.Result
		}

		// Note: text from assistant messages is already captured above via event.Text
	}

	return text, result, nil
}

// CollectText is a helper that extracts all text from a stream of events.
func CollectText(events <-chan Event) (string, error) {
	var text string
	for event := range events {
		if event.Error != nil {
			return text, event.Error
		}
		if event.Text != "" {
			text += event.Text
		}
	}
	return text, nil
}

// StreamCallback is a function called for each streaming event.
type StreamCallback func(event Event) error

// QueryWithCallback sends a prompt and calls the callback for each event.
func QueryWithCallback(ctx context.Context, prompt string, callback StreamCallback, opts ...Options) error {
	events, err := Query(ctx, prompt, opts...)
	if err != nil {
		return err
	}

	for event := range events {
		if event.Error != nil {
			return event.Error
		}
		if err := callback(event); err != nil {
			return err
		}
	}

	return nil
}
