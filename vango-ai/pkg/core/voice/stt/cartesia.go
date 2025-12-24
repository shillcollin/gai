package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
)

const (
	cartesiaBaseURL = "https://api.cartesia.ai"
	cartesiaVersion = "2025-04-16"
)

// CartesiaProvider implements the STT Provider interface using Cartesia's API.
type CartesiaProvider struct {
	apiKey     string
	httpClient *http.Client
}

// NewCartesia creates a new Cartesia STT provider.
func NewCartesia(apiKey string) *CartesiaProvider {
	return &CartesiaProvider{
		apiKey:     apiKey,
		httpClient: &http.Client{},
	}
}

// NewCartesiaWithClient creates a new Cartesia STT provider with a custom HTTP client.
func NewCartesiaWithClient(apiKey string, client *http.Client) *CartesiaProvider {
	return &CartesiaProvider{
		apiKey:     apiKey,
		httpClient: client,
	}
}

// Name returns the provider identifier.
func (c *CartesiaProvider) Name() string {
	return "cartesia"
}

// Transcribe converts audio to text using Cartesia's STT API.
func (c *CartesiaProvider) Transcribe(ctx context.Context, audio io.Reader, opts TranscribeOptions) (*Transcript, error) {
	// Read audio data
	audioData, err := io.ReadAll(audio)
	if err != nil {
		return nil, fmt.Errorf("read audio: %w", err)
	}

	// Build multipart form
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	// Add audio file
	ext := getExtension(opts.Format)
	fw, err := mw.CreateFormFile("file", "audio."+ext)
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := fw.Write(audioData); err != nil {
		return nil, fmt.Errorf("write audio data: %w", err)
	}

	// Add model (default to ink-whisper)
	model := opts.Model
	if model == "" {
		model = "ink-whisper"
	}
	if err := mw.WriteField("model", model); err != nil {
		return nil, fmt.Errorf("write model field: %w", err)
	}

	// Add language if specified
	if opts.Language != "" {
		if err := mw.WriteField("language", opts.Language); err != nil {
			return nil, fmt.Errorf("write language field: %w", err)
		}
	}

	// Request word timestamps if enabled
	if opts.Timestamps {
		if err := mw.WriteField("timestamp_granularities[]", "word"); err != nil {
			return nil, fmt.Errorf("write timestamp field: %w", err)
		}
	}

	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	// Build URL with optional query params
	reqURL := cartesiaBaseURL + "/stt"
	if opts.Format != "" || opts.SampleRate > 0 {
		u, _ := url.Parse(reqURL)
		q := u.Query()
		if encoding := getEncoding(opts.Format); encoding != "" {
			q.Set("encoding", encoding)
		}
		if opts.SampleRate > 0 {
			q.Set("sample_rate", fmt.Sprintf("%d", opts.SampleRate))
		}
		u.RawQuery = q.Encode()
		reqURL = u.String()
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, &buf)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Cartesia-Version", cartesiaVersion)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	// Execute
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cartesia request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cartesia error %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var cartesiaResp cartesiaTranscriptionResponse
	if err := json.NewDecoder(resp.Body).Decode(&cartesiaResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return c.convertResponse(cartesiaResp), nil
}

type cartesiaTranscriptionResponse struct {
	Text     string   `json:"text"`
	Language *string  `json:"language,omitempty"`
	Duration *float64 `json:"duration,omitempty"`
	Words    []struct {
		Word  string  `json:"word"`
		Start float64 `json:"start"`
		End   float64 `json:"end"`
	} `json:"words,omitempty"`
}

func (c *CartesiaProvider) convertResponse(resp cartesiaTranscriptionResponse) *Transcript {
	t := &Transcript{
		Text: resp.Text,
	}

	if resp.Language != nil {
		t.Language = *resp.Language
	}
	if resp.Duration != nil {
		t.Duration = *resp.Duration
	}

	if len(resp.Words) > 0 {
		t.Words = make([]Word, len(resp.Words))
		for i, w := range resp.Words {
			t.Words[i] = Word{
				Word:  w.Word,
				Start: w.Start,
				End:   w.End,
			}
		}
	}

	return t
}

// TranscribeStream transcribes streaming audio.
// Note: Cartesia streaming STT uses WebSocket at /stt/ws - not yet implemented.
func (c *CartesiaProvider) TranscribeStream(ctx context.Context, audio io.Reader, opts TranscribeOptions) (<-chan TranscriptDelta, error) {
	// Future: WebSocket streaming at /stt/ws
	return nil, fmt.Errorf("streaming transcription not yet implemented for Cartesia")
}

// getExtension returns the file extension for the given audio format.
func getExtension(format string) string {
	switch format {
	case "wav", "mp3", "webm", "ogg", "flac", "m4a", "mp4", "mpeg", "mpga", "oga":
		return format
	default:
		return "wav"
	}
}

// getEncoding returns the PCM encoding for raw audio formats.
func getEncoding(format string) string {
	switch format {
	case "pcm_s16le", "pcm_s32le", "pcm_f16le", "pcm_f32le", "pcm_mulaw", "pcm_alaw":
		return format
	default:
		return ""
	}
}
