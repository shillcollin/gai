package core

import (
	"bytes"
	"io"
)

// CollectStream drains a stream and returns the aggregated TextResult.
func CollectStream(stream *Stream) (*TextResult, error) {
	if stream == nil {
		return nil, ErrStreamClosed
	}
	buf := bytes.NewBuffer(nil)
	var result TextResult
	for event := range stream.Events() {
		switch event.Type {
		case EventTextDelta:
			buf.WriteString(event.TextDelta)
		case EventFinish:
			if event.FinishReason != nil {
				result.FinishReason = *event.FinishReason
			}
			result.Usage = event.Usage
			result.Model = event.Model
			result.Provider = event.Provider
		case EventError:
			if event.Error != nil {
				return nil, event.Error
			}
		}
	}
	if err := stream.Err(); err != nil {
		return nil, err
	}
	result.Text = buf.String()
	return &result, nil
}

// StreamToWriter writes streaming text events to the provided writer.
func StreamToWriter(stream *Stream, w io.Writer) error {
	for event := range stream.Events() {
		if event.Type == EventTextDelta {
			if _, err := io.WriteString(w, event.TextDelta); err != nil {
				return err
			}
		}
		if event.Type == EventError && event.Error != nil {
			return event.Error
		}
	}
	return stream.Err()
}
