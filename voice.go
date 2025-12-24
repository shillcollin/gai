package gai

import (
	"context"
	"fmt"

	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/tts"
)

// GenerateVoice performs a voice pipeline: STT (if audio input) -> LLM -> TTS (if voice output).
// This is the main method for voice-enabled generation.
//
// Example - Full voice pipeline:
//
//	result, err := client.GenerateVoice(ctx,
//	    gai.Request("anthropic/claude-3-5-sonnet").
//	        Audio(speechBytes, "audio/wav").
//	        STT("deepgram/nova-2").
//	        Voice("elevenlabs/rachel"))
//
//	fmt.Println("User said:", result.Transcript())
//	fmt.Println("Assistant:", result.Text())
//	playAudio(result.AudioData())
//
// Example - Text input with voice output:
//
//	result, err := client.GenerateVoice(ctx,
//	    gai.Request("openai/gpt-4o").
//	        User("Tell me a joke").
//	        Voice("cartesia/sonic-english"))
func (c *Client) GenerateVoice(ctx context.Context, req *RequestBuilder) (*Result, error) {
	var transcript string

	// Step 1: Handle STT if there's audio input
	audioData, audioMIME := extractAudioFromRequest(req)
	if audioData != nil {
		sttProvider := req.sttProvider
		if sttProvider == "" {
			sttProvider = c.defaults.STT
		}

		text, err := c.Transcribe(ctx, audioData, WithSTT(sttProvider))
		if err != nil {
			return nil, fmt.Errorf("STT transcription failed: %w", err)
		}

		transcript = text

		// Replace audio with transcribed text
		req = replaceAudioWithText(req, text, audioMIME)
	}

	// Step 2: Generate LLM response (call provider directly to avoid recursion)
	provider, modelID, err := c.resolveModel(req.model)
	if err != nil {
		return nil, err
	}

	coreReq := req.build(modelID, c.defaults)
	coreResult, err := provider.GenerateText(ctx, coreReq)
	if err != nil {
		return nil, &ProviderError{
			Provider: req.model,
			Op:       "GenerateText",
			Err:      err,
		}
	}
	result := newResult(coreResult)

	// Set transcript if we did STT
	if transcript != "" {
		result.SetTranscript(transcript)
	}

	// Step 3: Handle TTS if voice output is requested
	voiceOutput := req.voiceOutput
	if voiceOutput == "" {
		voiceOutput = c.defaults.Voice
	}

	if voiceOutput != "" && result.HasText() {
		audio, err := c.SynthesizeFull(ctx, result.Text(), WithTTS(voiceOutput))
		if err != nil {
			return nil, fmt.Errorf("TTS synthesis failed: %w", err)
		}

		result.SetAudioData(audio.Data, string(audio.Format))
	}

	return result, nil
}

// StreamVoice streams a voice pipeline response.
// Text is streamed from the LLM, and if voice output is requested,
// audio synthesis happens in parallel.
//
// Example:
//
//	stream, err := client.StreamVoice(ctx,
//	    gai.Request("anthropic/claude-3-5-sonnet").
//	        User("Tell me a story").
//	        Voice("elevenlabs/rachel"))
//
//	for chunk := range stream.TextChunks() {
//	    fmt.Print(chunk)
//	}
//
//	// After streaming completes, get synthesized audio
//	result := stream.Result()
//	playAudio(result.AudioData())
func (c *Client) StreamVoice(ctx context.Context, req *RequestBuilder) (*VoiceStream, error) {
	var transcript string

	// Step 1: Handle STT if there's audio input
	audioData, audioMIME := extractAudioFromRequest(req)
	if audioData != nil {
		sttProvider := req.sttProvider
		if sttProvider == "" {
			sttProvider = c.defaults.STT
		}

		text, err := c.Transcribe(ctx, audioData, WithSTT(sttProvider))
		if err != nil {
			return nil, fmt.Errorf("STT transcription failed: %w", err)
		}

		transcript = text

		// Replace audio with transcribed text
		req = replaceAudioWithText(req, text, audioMIME)
	}

	// Step 2: Start LLM streaming
	stream, err := c.Stream(ctx, req)
	if err != nil {
		return nil, err
	}

	// Step 3: Determine if we need TTS
	voiceOutput := req.voiceOutput
	if voiceOutput == "" {
		voiceOutput = c.defaults.Voice
	}

	vs := &VoiceStream{
		stream:     stream,
		client:     c,
		voice:      voiceOutput,
		transcript: transcript,
	}

	return vs, nil
}

// VoiceStream wraps a Stream with voice pipeline capabilities.
type VoiceStream struct {
	stream     *Stream
	client     *Client
	voice      string
	transcript string

	// Collected text for TTS
	collectedText string
	audioData     []byte
	audioMIME     string
	audioErr      error

	// Current event
	currentEvent core.StreamEvent
}

// Events returns the channel of streaming events.
func (vs *VoiceStream) Events() <-chan core.StreamEvent {
	return vs.stream.Events()
}

// CollectAndSynthesize collects all text from the stream and synthesizes audio.
// This is a convenience method that handles the common case.
func (vs *VoiceStream) CollectAndSynthesize(ctx context.Context) (*Result, error) {
	// Collect all text
	var text string
	for event := range vs.stream.Events() {
		if event.Type == core.EventTextDelta {
			text += event.TextDelta
		}
	}

	if err := vs.stream.Err(); err != nil && err != core.ErrStreamClosed {
		return nil, err
	}

	// Create result
	result := &Result{
		inner: &core.TextResult{
			Text: text,
		},
	}

	// Set transcript if we did STT
	if vs.transcript != "" {
		result.SetTranscript(vs.transcript)
	}

	// Synthesize audio if voice is configured
	if vs.voice != "" && text != "" {
		audio, err := vs.client.SynthesizeFull(ctx, text, WithTTS(vs.voice))
		if err != nil {
			return nil, fmt.Errorf("TTS synthesis failed: %w", err)
		}
		result.SetAudioData(audio.Data, string(audio.Format))
	}

	return result, nil
}

// Err returns any error from the stream.
func (vs *VoiceStream) Err() error {
	return vs.stream.Err()
}

// Close closes the stream.
func (vs *VoiceStream) Close() error {
	return vs.stream.Close()
}

// Transcript returns the STT transcript (if audio was transcribed).
func (vs *VoiceStream) Transcript() string {
	return vs.transcript
}

// extractAudioFromRequest finds audio content in the request messages.
func extractAudioFromRequest(req *RequestBuilder) ([]byte, string) {
	for _, msg := range req.messages {
		for _, part := range msg.Parts {
			if audio, ok := part.(core.Audio); ok {
				if audio.Source.Kind == core.BlobBytes {
					return audio.Source.Bytes, audio.Source.MIME
				}
			}
		}
	}
	return nil, ""
}

// replaceAudioWithText creates a new request with audio replaced by transcribed text.
func replaceAudioWithText(req *RequestBuilder, text, _ string) *RequestBuilder {
	// Create a new builder with same settings
	newReq := &RequestBuilder{
		model:        req.model,
		tools:        req.tools,
		toolChoice:   req.toolChoice,
		outputSchema: req.outputSchema,
		temperature:  req.temperature,
		maxTokens:    req.maxTokens,
		topP:         req.topP,
		topK:         req.topK,
		stopWhen:     req.stopWhen,
		onStop:       req.onStop,
		metadata:     req.metadata,
		providerOpts: req.providerOpts,
		voiceOutput:  req.voiceOutput,
		sttProvider:  req.sttProvider,
	}

	// Copy messages, replacing audio parts with transcribed text
	for _, msg := range req.messages {
		newMsg := core.Message{
			Role:  msg.Role,
			Parts: make([]core.Part, 0, len(msg.Parts)),
		}

		for _, part := range msg.Parts {
			if _, ok := part.(core.Audio); ok {
				// Replace audio with transcribed text
				newMsg.Parts = append(newMsg.Parts, core.Text{
					Text: fmt.Sprintf("[Transcribed speech]: %s", text),
				})
			} else {
				newMsg.Parts = append(newMsg.Parts, part)
			}
		}

		newReq.messages = append(newReq.messages, newMsg)
	}

	return newReq
}

// StreamVoiceRealtime streams TTS in real-time as text chunks arrive.
// This provides lower latency than waiting for the full response.
//
// Example:
//
//	stream, err := client.StreamVoiceRealtime(ctx,
//	    gai.Request("anthropic/claude-3-5-sonnet").
//	        User("Tell me a story").
//	        Voice("cartesia/sonic-english")) // Cartesia is optimized for streaming
//
//	// Process LLM events and TTS audio events
//	for event := range stream.Events() {
//	    // Handle text and audio events
//	}
func (c *Client) StreamVoiceRealtime(ctx context.Context, req *RequestBuilder) (*RealtimeVoiceStream, error) {
	var transcript string

	// Handle STT if there's audio input
	audioData, audioMIME := extractAudioFromRequest(req)
	if audioData != nil {
		sttProvider := req.sttProvider
		if sttProvider == "" {
			sttProvider = c.defaults.STT
		}

		text, err := c.Transcribe(ctx, audioData, WithSTT(sttProvider))
		if err != nil {
			return nil, fmt.Errorf("STT transcription failed: %w", err)
		}

		transcript = text
		req = replaceAudioWithText(req, text, audioMIME)
	}

	// Determine voice
	voiceOutput := req.voiceOutput
	if voiceOutput == "" {
		voiceOutput = c.defaults.Voice
	}

	// Start LLM streaming
	llmStream, err := c.Stream(ctx, req)
	if err != nil {
		return nil, err
	}

	// Create text channel for TTS
	textChan := make(chan string, 100)

	// Start TTS streaming if voice is configured
	var ttsStream *tts.AudioStream
	if voiceOutput != "" {
		ttsStream, err = c.StreamSynthesize(ctx, textChan, WithTTS(voiceOutput))
		if err != nil {
			llmStream.Close()
			return nil, fmt.Errorf("TTS stream failed: %w", err)
		}
	}

	rvs := &RealtimeVoiceStream{
		llmStream:  llmStream,
		ttsStream:  ttsStream,
		textChan:   textChan,
		transcript: transcript,
		voice:      voiceOutput,
	}

	// Start goroutine to pipe LLM text to TTS
	go rvs.pipeLLMToTTS()

	return rvs, nil
}

// RealtimeVoiceStream provides real-time TTS as LLM generates text.
type RealtimeVoiceStream struct {
	llmStream  *Stream
	ttsStream  *tts.AudioStream
	textChan   chan string
	transcript string
	voice      string

	// Current state
	err    error
	closed bool
}

// pipeLLMToTTS reads from LLM stream and sends text to TTS.
func (rvs *RealtimeVoiceStream) pipeLLMToTTS() {
	defer close(rvs.textChan)

	for event := range rvs.llmStream.Events() {
		if event.Type == core.EventTextDelta && rvs.ttsStream != nil {
			select {
			case rvs.textChan <- event.TextDelta:
			default:
				// Channel full, skip (shouldn't happen with sufficient buffer)
			}
		}
	}
}

// LLMEvents returns the channel of LLM streaming events.
func (rvs *RealtimeVoiceStream) LLMEvents() <-chan core.StreamEvent {
	return rvs.llmStream.Events()
}

// TTSEvents returns the channel of TTS audio events.
func (rvs *RealtimeVoiceStream) TTSEvents() <-chan tts.AudioEvent {
	if rvs.ttsStream == nil {
		return nil
	}
	return rvs.ttsStream.Events()
}

// Transcript returns the STT transcript.
func (rvs *RealtimeVoiceStream) Transcript() string {
	return rvs.transcript
}

// Err returns any error from the streams.
func (rvs *RealtimeVoiceStream) Err() error {
	if rvs.err != nil {
		return rvs.err
	}
	if rvs.llmStream != nil && rvs.llmStream.Err() != nil {
		return rvs.llmStream.Err()
	}
	if rvs.ttsStream != nil && rvs.ttsStream.Err() != nil {
		return rvs.ttsStream.Err()
	}
	return nil
}

// Close closes all streams.
func (rvs *RealtimeVoiceStream) Close() error {
	rvs.closed = true
	var err error
	if rvs.llmStream != nil {
		if e := rvs.llmStream.Close(); e != nil {
			err = e
		}
	}
	if rvs.ttsStream != nil {
		if e := rvs.ttsStream.Close(); e != nil && err == nil {
			err = e
		}
	}
	return err
}
