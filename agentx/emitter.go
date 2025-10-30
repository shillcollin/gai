package agentx

import (
	"fmt"
	"io"
	"sync"
	"time"

	agentEvents "github.com/shillcollin/gai/agentx/events"
)

// Emitter defines the interface for event emission.
type Emitter interface {
	Emit(event agentEvents.AgentEventV1) error
}

// MultiEmitter sends events to multiple emitters.
type MultiEmitter struct {
	emitters []Emitter
}

// NewMultiEmitter creates a multi-emitter that fans out to all provided emitters.
func NewMultiEmitter(emitters ...Emitter) *MultiEmitter {
	return &MultiEmitter{emitters: emitters}
}

// Emit sends the event to all registered emitters.
// If any emitter fails, the error is returned but other emitters still receive the event.
func (m *MultiEmitter) Emit(event agentEvents.AgentEventV1) error {
	var firstErr error
	for _, emitter := range m.emitters {
		if err := emitter.Emit(event); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ChannelEmitter sends events to a Go channel.
type ChannelEmitter struct {
	ch chan<- agentEvents.AgentEventV1
}

// NewChannelEmitter creates an emitter that sends events to a channel.
func NewChannelEmitter(ch chan<- agentEvents.AgentEventV1) *ChannelEmitter {
	return &ChannelEmitter{ch: ch}
}

// Emit sends the event to the channel (non-blocking).
func (c *ChannelEmitter) Emit(event agentEvents.AgentEventV1) error {
	select {
	case c.ch <- event:
		return nil
	default:
		// Don't block if channel is full
		return fmt.Errorf("channel emitter: channel full, event dropped")
	}
}

// ConsoleEmitter writes formatted events to a writer (typically os.Stdout or os.Stderr).
type ConsoleEmitter struct {
	w      io.Writer
	format ConsoleFormat
	mu     sync.Mutex
}

// ConsoleFormat defines output formatting style.
type ConsoleFormat int

const (
	// ConsoleFormatSimple outputs one line per event.
	ConsoleFormatSimple ConsoleFormat = iota
	// ConsoleFormatPretty outputs formatted, human-readable output.
	ConsoleFormatPretty
	// ConsoleFormatJSON outputs raw JSON per event.
	ConsoleFormatJSON
)

// NewConsoleEmitter creates an emitter that writes to a writer.
func NewConsoleEmitter(w io.Writer, format ConsoleFormat) *ConsoleEmitter {
	return &ConsoleEmitter{w: w, format: format}
}

// Emit formats and writes the event to the writer.
func (c *ConsoleEmitter) Emit(event agentEvents.AgentEventV1) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var output string

	switch c.format {
	case ConsoleFormatSimple:
		output = c.formatSimple(event)
	case ConsoleFormatPretty:
		output = c.formatPretty(event)
	case ConsoleFormatJSON:
		output = c.formatJSON(event)
	default:
		output = c.formatSimple(event)
	}

	_, err := fmt.Fprint(c.w, output)
	return err
}

func (c *ConsoleEmitter) formatSimple(event agentEvents.AgentEventV1) string {
	ts := time.UnixMilli(event.Ts).Format("15:04:05")
	return fmt.Sprintf("[%s] %s %s\n", ts, event.Type, event.Step)
}

func (c *ConsoleEmitter) formatPretty(event agentEvents.AgentEventV1) string {
	ts := time.UnixMilli(event.Ts).Format("15:04:05")

	var icon string
	switch event.Type {
	case agentEvents.TypePhaseStart:
		icon = "⏳"
	case agentEvents.TypePhaseFinish:
		icon = "✅"
	case agentEvents.TypePhaseSkipped:
		icon = "⏭️"
	case agentEvents.TypeApprovalRequested:
		icon = "⚠️"
	case agentEvents.TypeApprovalDecided:
		icon = "✓"
	case agentEvents.TypeError:
		icon = "❌"
	case agentEvents.TypeUsageDelta:
		icon = "📊"
	case agentEvents.TypeFinish:
		icon = "🎉"
	default:
		icon = "•"
	}

	var msg string
	if event.Progress != nil && event.Progress.TotalPhases > 0 {
		msg = fmt.Sprintf("[%s] %s Phase %d/%d: %s",
			ts, icon, event.Progress.CurrentPhaseIndex+1,
			event.Progress.TotalPhases, event.Progress.CurrentPhase)
		if event.Progress.PercentComplete > 0 {
			msg += fmt.Sprintf(" (%.0f%%)", event.Progress.PercentComplete)
		}
	} else {
		msg = fmt.Sprintf("[%s] %s %s", ts, icon, event.Step)
	}

	if event.Message != "" {
		msg += " - " + event.Message
	}
	if event.Error != "" {
		msg += " - ERROR: " + event.Error
	}

	return msg + "\n"
}

func (c *ConsoleEmitter) formatJSON(event agentEvents.AgentEventV1) string {
	// Use the event's built-in JSON marshaling
	data, err := event.MarshalJSON()
	if err != nil {
		return fmt.Sprintf(`{"error":"failed to marshal event: %v"}`+"\n", err)
	}
	return string(data) + "\n"
}

// CallbackEmitter invokes a callback function for each event.
type CallbackEmitter struct {
	fn func(agentEvents.AgentEventV1)
}

// NewCallbackEmitter creates an emitter that calls a function.
func NewCallbackEmitter(fn func(agentEvents.AgentEventV1)) *CallbackEmitter {
	return &CallbackEmitter{fn: fn}
}

// Emit invokes the callback with the event.
func (c *CallbackEmitter) Emit(event agentEvents.AgentEventV1) error {
	c.fn(event)
	return nil
}
