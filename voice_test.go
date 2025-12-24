package gai

import (
	"context"
	"testing"

	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/internal/testutil"
	"github.com/shillcollin/gai/stt"
	"github.com/shillcollin/gai/tts"
)

func TestRequestBuilderVoice(t *testing.T) {
	req := Request("test/model").
		User("Hello").
		Voice("elevenlabs/rachel")

	if req.VoiceOutput() != "elevenlabs/rachel" {
		t.Errorf("expected voice output 'elevenlabs/rachel', got %q", req.VoiceOutput())
	}
}

func TestRequestBuilderSTT(t *testing.T) {
	req := Request("test/model").
		Audio([]byte("fake audio"), "audio/wav").
		STT("deepgram/nova-2")

	if req.STTProvider() != "deepgram/nova-2" {
		t.Errorf("expected STT provider 'deepgram/nova-2', got %q", req.STTProvider())
	}
}

func TestResultTranscript(t *testing.T) {
	result := &Result{
		inner: &core.TextResult{
			Text: "Hello world",
		},
	}

	// Initially no transcript
	if result.HasTranscript() {
		t.Error("expected no transcript initially")
	}

	// Set transcript
	result.SetTranscript("User said hello")

	if !result.HasTranscript() {
		t.Error("expected transcript after setting")
	}

	if result.Transcript() != "User said hello" {
		t.Errorf("expected transcript 'User said hello', got %q", result.Transcript())
	}
}

func TestResultAudioData(t *testing.T) {
	result := &Result{
		inner: &core.TextResult{
			Text: "Hello world",
		},
	}

	// Initially no audio
	if result.HasAudio() {
		t.Error("expected no audio initially")
	}

	// Set audio
	audioData := []byte{1, 2, 3, 4}
	result.SetAudioData(audioData, "audio/mpeg")

	if !result.HasAudio() {
		t.Error("expected audio after setting")
	}

	if string(result.AudioData()) != string(audioData) {
		t.Error("audio data mismatch")
	}

	if result.AudioMIME() != "audio/mpeg" {
		t.Errorf("expected MIME 'audio/mpeg', got %q", result.AudioMIME())
	}
}

func TestExtractAudioFromRequest(t *testing.T) {
	// Request with no audio
	req := Request("test/model").User("Hello")
	data, mime := extractAudioFromRequest(req)
	if data != nil {
		t.Error("expected nil audio data for text-only request")
	}

	// Request with audio
	audioBytes := []byte("fake audio data")
	req = Request("test/model").Audio(audioBytes, "audio/wav")
	data, mime = extractAudioFromRequest(req)

	if data == nil {
		t.Fatal("expected audio data")
	}
	if string(data) != string(audioBytes) {
		t.Error("audio data mismatch")
	}
	if mime != "audio/wav" {
		t.Errorf("expected mime 'audio/wav', got %q", mime)
	}
}

func TestReplaceAudioWithText(t *testing.T) {
	audioBytes := []byte("fake audio")
	req := Request("test/model").
		System("You are helpful").
		Audio(audioBytes, "audio/wav").
		Voice("elevenlabs/rachel")

	newReq := replaceAudioWithText(req, "Hello world", "audio/wav")

	// Check voice output is preserved
	if newReq.VoiceOutput() != "elevenlabs/rachel" {
		t.Errorf("expected voice preserved, got %q", newReq.VoiceOutput())
	}

	// Check model is preserved
	if newReq.Model() != "test/model" {
		t.Errorf("expected model preserved, got %q", newReq.Model())
	}

	// Check audio was replaced with text
	hasAudio := false
	hasTranscribedText := false
	for _, msg := range newReq.messages {
		for _, part := range msg.Parts {
			if _, ok := part.(core.Audio); ok {
				hasAudio = true
			}
			if text, ok := part.(core.Text); ok && text.Text == "[Transcribed speech]: Hello world" {
				hasTranscribedText = true
			}
		}
	}

	if hasAudio {
		t.Error("expected audio to be removed")
	}
	if !hasTranscribedText {
		t.Error("expected transcribed text to be added")
	}
}

func TestClientTranscribeWithMock(t *testing.T) {
	mockSTT := testutil.NewMockSTT()
	mockSTT.DefaultText = "Hello from mock STT"

	client := &Client{
		sttProviders: map[string]stt.Provider{
			"mock": mockSTT,
		},
		defaults: ClientDefaults{
			STT: "mock",
		},
	}

	text, err := client.Transcribe(context.Background(), []byte("audio"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if text != "Hello from mock STT" {
		t.Errorf("expected 'Hello from mock STT', got %q", text)
	}

	// Verify the call was recorded
	if len(mockSTT.TranscribeCalls) != 1 {
		t.Errorf("expected 1 call, got %d", len(mockSTT.TranscribeCalls))
	}
}

func TestClientSynthesizeWithMock(t *testing.T) {
	mockTTS := testutil.NewMockTTS()

	client := &Client{
		ttsProviders: map[string]tts.Provider{
			"mock": mockTTS,
		},
		defaults: ClientDefaults{
			Voice: "mock/default",
		},
	}

	audio, err := client.Synthesize(context.Background(), "Hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(audio) == 0 {
		t.Error("expected non-empty audio data")
	}

	// Verify the call was recorded
	if len(mockTTS.SynthesizeCalls) != 1 {
		t.Errorf("expected 1 call, got %d", len(mockTTS.SynthesizeCalls))
	}

	if mockTTS.SynthesizeCalls[0].Text != "Hello world" {
		t.Errorf("expected text 'Hello world', got %q", mockTTS.SynthesizeCalls[0].Text)
	}
}

func TestClientWithSTTAPIKey(t *testing.T) {
	// This tests the option pattern, not actual API calls
	client := &Client{
		sttProviders: make(map[string]stt.Provider),
	}

	opt := WithSTTProvider("test", testutil.NewMockSTT())
	opt(client)

	if !client.HasSTTProvider("test") {
		t.Error("expected STT provider 'test' to be configured")
	}
}

func TestClientWithTTSAPIKey(t *testing.T) {
	// This tests the option pattern, not actual API calls
	client := &Client{
		ttsProviders: make(map[string]tts.Provider),
	}

	opt := WithTTSProvider("test", testutil.NewMockTTS())
	opt(client)

	if !client.HasTTSProvider("test") {
		t.Error("expected TTS provider 'test' to be configured")
	}
}

func TestVoiceConfig(t *testing.T) {
	config := VoiceConfig{
		STT: "deepgram/nova-2",
		LLM: "anthropic/claude-3-5-sonnet",
		TTS: "elevenlabs/rachel",
	}

	if config.STT != "deepgram/nova-2" {
		t.Errorf("expected STT 'deepgram/nova-2', got %q", config.STT)
	}
	if config.LLM != "anthropic/claude-3-5-sonnet" {
		t.Errorf("expected LLM 'anthropic/claude-3-5-sonnet', got %q", config.LLM)
	}
	if config.TTS != "elevenlabs/rachel" {
		t.Errorf("expected TTS 'elevenlabs/rachel', got %q", config.TTS)
	}
}

func TestWithVoiceAlias(t *testing.T) {
	client := &Client{
		voiceAliases: make(map[string]VoiceConfig),
	}

	opt := WithVoiceAlias("support", VoiceConfig{
		STT: "deepgram/nova-2",
		LLM: "anthropic/claude-3-5-sonnet",
		TTS: "elevenlabs/rachel",
	})
	opt(client)

	alias, ok := client.voiceAliases["support"]
	if !ok {
		t.Fatal("expected 'support' alias to be configured")
	}

	if alias.TTS != "elevenlabs/rachel" {
		t.Errorf("expected TTS 'elevenlabs/rachel', got %q", alias.TTS)
	}
}

func TestTranscribeOptions(t *testing.T) {
	opts := &transcribeOptions{}

	WithSTT("deepgram/nova-2")(opts)
	WithTranscribeModel("nova-2")(opts)
	WithTranscribeLanguage("en-US")(opts)
	WithDiarization()(opts)
	WithWordTimestamps()(opts)

	if opts.sttProvider != "deepgram/nova-2" {
		t.Errorf("expected provider 'deepgram/nova-2', got %q", opts.sttProvider)
	}
	if opts.model != "nova-2" {
		t.Errorf("expected model 'nova-2', got %q", opts.model)
	}
	if opts.language != "en-US" {
		t.Errorf("expected language 'en-US', got %q", opts.language)
	}
	if !opts.diarize {
		t.Error("expected diarize to be true")
	}
	if !opts.timestamps {
		t.Error("expected timestamps to be true")
	}
}

func TestSynthesizeOptions(t *testing.T) {
	opts := &synthesizeOptions{}

	WithTTS("elevenlabs")(opts)
	WithVoice("rachel")(opts)
	WithSynthesizeModel("eleven_multilingual_v2")(opts)
	WithSynthesizeSpeed(1.2)(opts)
	WithAudioFormat(tts.FormatMP3)(opts)
	WithSampleRate(44100)(opts)

	if opts.ttsProvider != "elevenlabs" {
		t.Errorf("expected provider 'elevenlabs', got %q", opts.ttsProvider)
	}
	if opts.voice != "rachel" {
		t.Errorf("expected voice 'rachel', got %q", opts.voice)
	}
	if opts.model != "eleven_multilingual_v2" {
		t.Errorf("expected model 'eleven_multilingual_v2', got %q", opts.model)
	}
	if opts.speed != 1.2 {
		t.Errorf("expected speed 1.2, got %f", opts.speed)
	}
	if opts.format != tts.FormatMP3 {
		t.Errorf("expected format mp3, got %s", opts.format)
	}
	if opts.sampleRate != 44100 {
		t.Errorf("expected sample rate 44100, got %d", opts.sampleRate)
	}
}
