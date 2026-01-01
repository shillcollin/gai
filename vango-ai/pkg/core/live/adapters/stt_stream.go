package adapters

import (
	"context"
	"io"
	"strings"
	"sync"

	"github.com/vango-ai/vango/pkg/core/live"
	"github.com/vango-ai/vango/pkg/core/voice/stt"
)

// StreamingSTTStream adapts a voice/stt.Provider TranscribeStream() into the live.STTStream interface.
// It uses an io.Pipe so callers can Write() audio incrementally.
type StreamingSTTStream struct {
	ctx context.Context

	pipeR *io.PipeReader
	pipeW *io.PipeWriter

	mu sync.Mutex
	// finalTranscript accumulates finalized segments (space-separated).
	finalTranscript strings.Builder
	// currentSegment is the latest non-final segment.
	currentSegment string
	// lastDelta is the most recent segment received (final or partial), cleared on TranscriptDelta().
	lastDelta string

	done chan struct{}
}

type StreamingSTTStreamConfig struct {
	Provider stt.Provider
	Input    *live.VoiceInputConfig
}

func NewStreamingSTTStream(ctx context.Context, cfg StreamingSTTStreamConfig) (*StreamingSTTStream, error) {
	pipeR, pipeW := io.Pipe()

	opts := stt.TranscribeOptions{
		Model:      "",
		Language:   "",
		Format:     "pcm_s16le",
		SampleRate: 16000,
	}
	if cfg.Input != nil {
		if cfg.Input.Model != "" {
			opts.Model = cfg.Input.Model
		}
		if cfg.Input.Language != "" {
			opts.Language = cfg.Input.Language
		}
	}

	transcripts, err := cfg.Provider.TranscribeStream(ctx, pipeR, opts)
	if err != nil {
		pipeR.Close()
		pipeW.Close()
		return nil, err
	}

	s := &StreamingSTTStream{
		ctx:   ctx,
		pipeR: pipeR,
		pipeW: pipeW,
		done:  make(chan struct{}),
	}

	go s.readLoop(transcripts)

	return s, nil
}

func (s *StreamingSTTStream) readLoop(ch <-chan stt.TranscriptDelta) {
	defer close(s.done)

	for {
		select {
		case <-s.ctx.Done():
			return
		case delta, ok := <-ch:
			if !ok {
				return
			}

			text := strings.TrimSpace(delta.Text)
			if text == "" {
				continue
			}

			s.mu.Lock()
			s.lastDelta = delta.Text
			if delta.IsFinal {
				if s.finalTranscript.Len() > 0 {
					s.finalTranscript.WriteString(" ")
				}
				s.finalTranscript.WriteString(text)
				s.currentSegment = ""
			} else {
				s.currentSegment = text
			}
			s.mu.Unlock()
		}
	}
}

func (s *StreamingSTTStream) Write(audio []byte) error {
	_, err := s.pipeW.Write(audio)
	return err
}

func (s *StreamingSTTStream) Transcript() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	finalText := strings.TrimSpace(s.finalTranscript.String())
	segment := strings.TrimSpace(s.currentSegment)

	switch {
	case finalText == "":
		return segment
	case segment == "":
		return finalText
	default:
		return finalText + " " + segment
	}
}

func (s *StreamingSTTStream) TranscriptDelta() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	delta := s.lastDelta
	s.lastDelta = ""
	return delta
}

func (s *StreamingSTTStream) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finalTranscript.Reset()
	s.currentSegment = ""
	s.lastDelta = ""
}

func (s *StreamingSTTStream) Close() error {
	_ = s.pipeW.Close()
	_ = s.pipeR.Close()
	<-s.done
	return nil
}

var _ live.STTStream = (*StreamingSTTStream)(nil)
