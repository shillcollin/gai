// Package deepgram provides a Deepgram STT provider implementation.
package deepgram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shillcollin/gai/stt"
)

const (
	defaultBaseURL = "https://api.deepgram.com"
	defaultModel   = "nova-2"
)

// Client is a Deepgram STT provider.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	model      string
}

// Option configures a Deepgram client.
type Option func(*Client)

// WithAPIKey sets the API key.
func WithAPIKey(key string) Option {
	return func(c *Client) {
		c.apiKey = key
	}
}

// WithBaseURL sets the base URL.
func WithBaseURL(url string) Option {
	return func(c *Client) {
		c.baseURL = strings.TrimSuffix(url, "/")
	}
}

// WithHTTPClient sets the HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		c.httpClient = client
	}
}

// WithModel sets the default model.
func WithModel(model string) Option {
	return func(c *Client) {
		c.model = model
	}
}

// New creates a new Deepgram client.
func New(opts ...Option) *Client {
	c := &Client{
		baseURL:    defaultBaseURL,
		httpClient: http.DefaultClient,
		model:      defaultModel,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Transcribe implements stt.Provider.
func (c *Client) Transcribe(ctx context.Context, audio []byte, opts stt.Options) (*stt.Transcript, error) {
	// Build query parameters
	params := c.buildParams(opts)

	// Build request URL
	reqURL := fmt.Sprintf("%s/v1/listen?%s", c.baseURL, params.Encode())

	// Create request with audio body
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(audio))
	if err != nil {
		return nil, fmt.Errorf("deepgram: create request: %w", err)
	}

	// Set headers
	req.Header.Set("Authorization", "Token "+c.apiKey)
	req.Header.Set("Content-Type", "audio/wav") // Default, will be auto-detected

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("deepgram: request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("deepgram: read response: %w", err)
	}

	// Check for errors
	if resp.StatusCode != http.StatusOK {
		var errResp errorResponse
		if json.Unmarshal(body, &errResp) == nil && errResp.ErrMsg != "" {
			return nil, fmt.Errorf("deepgram: %s (code: %s)", errResp.ErrMsg, errResp.ErrCode)
		}
		return nil, fmt.Errorf("deepgram: unexpected status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var dgResp deepgramResponse
	if err := json.Unmarshal(body, &dgResp); err != nil {
		return nil, fmt.Errorf("deepgram: parse response: %w", err)
	}

	// Convert to stt.Transcript
	return c.toTranscript(&dgResp, opts), nil
}

// StreamTranscribe implements stt.Provider.
// Note: This is a simplified implementation that buffers audio and transcribes.
// For true real-time streaming, use WebSocket connection to wss://api.deepgram.com/v1/listen
func (c *Client) StreamTranscribe(ctx context.Context, audio io.Reader, opts stt.Options) (*stt.TranscriptStream, error) {
	stream := stt.NewTranscriptStream()

	go func() {
		defer stream.Close()

		// Buffer the audio (for simplified implementation)
		data, err := io.ReadAll(audio)
		if err != nil {
			stream.SetError(err)
			stream.Send(stt.TranscriptEvent{
				Type:      stt.TranscriptEventError,
				Error:     err,
				Timestamp: time.Now(),
			})
			return
		}

		// Transcribe
		result, err := c.Transcribe(ctx, data, opts)
		if err != nil {
			stream.SetError(err)
			stream.Send(stt.TranscriptEvent{
				Type:      stt.TranscriptEventError,
				Error:     err,
				Timestamp: time.Now(),
			})
			return
		}

		// Send final result
		stream.Send(stt.TranscriptEvent{
			Type:       stt.TranscriptEventFinal,
			Final:      result.Text,
			Words:      result.Words,
			IsFinal:    true,
			Confidence: result.Confidence,
			Timestamp:  time.Now(),
		})
	}()

	return stream, nil
}

// Capabilities implements stt.Provider.
func (c *Client) Capabilities() stt.Capabilities {
	return stt.Capabilities{
		Provider:    "deepgram",
		Models:      []string{"nova-2", "nova", "enhanced", "base"},
		Languages:   []string{"en", "en-US", "en-GB", "es", "fr", "de", "it", "pt", "nl", "ja", "ko", "zh"},
		Realtime:    true,
		Diarization: true,
		Timestamps:  true,
		MaxDuration: 0, // Unlimited (2GB file size limit)
	}
}

// buildParams builds URL query parameters from options.
func (c *Client) buildParams(opts stt.Options) url.Values {
	params := url.Values{}

	// Model
	model := opts.Model
	if model == "" {
		model = c.model
	}
	params.Set("model", model)

	// Language
	if opts.Language != "" {
		params.Set("language", opts.Language)
	}

	// Diarization
	if opts.Diarize {
		params.Set("diarize", "true")
	}

	// Timestamps (smart_format includes punctuation and formatting)
	params.Set("smart_format", "true")

	// Custom options
	for k, v := range opts.Custom {
		switch val := v.(type) {
		case string:
			params.Set(k, val)
		case bool:
			if val {
				params.Set(k, "true")
			}
		case int, int64, float64:
			params.Set(k, fmt.Sprintf("%v", val))
		}
	}

	return params
}

// toTranscript converts a Deepgram response to an stt.Transcript.
func (c *Client) toTranscript(resp *deepgramResponse, opts stt.Options) *stt.Transcript {
	transcript := &stt.Transcript{
		Provider: "deepgram",
		Model:    opts.Model,
	}

	if len(resp.Results.Channels) == 0 {
		return transcript
	}

	channel := resp.Results.Channels[0]
	if len(channel.Alternatives) == 0 {
		return transcript
	}

	alt := channel.Alternatives[0]
	transcript.Text = alt.Transcript
	transcript.Confidence = alt.Confidence

	// Duration from metadata
	if resp.Metadata.Duration > 0 {
		transcript.Duration = resp.Metadata.Duration
	}

	// Language
	if len(channel.DetectedLanguage) > 0 {
		transcript.Language = channel.DetectedLanguage
	} else if opts.Language != "" {
		transcript.Language = opts.Language
	}

	// Words
	if len(alt.Words) > 0 {
		transcript.Words = make([]stt.Word, len(alt.Words))
		for i, w := range alt.Words {
			speaker := -1
			if w.Speaker != nil {
				speaker = *w.Speaker
			}
			transcript.Words[i] = stt.Word{
				Text:       w.Word,
				Start:      w.Start,
				End:        w.End,
				Confidence: w.Confidence,
				Speaker:    speaker,
			}
		}
	}

	return transcript
}

// Deepgram API response types

type deepgramResponse struct {
	Metadata deepgramMetadata `json:"metadata"`
	Results  deepgramResults  `json:"results"`
}

type deepgramMetadata struct {
	TransactionKey string   `json:"transaction_key"`
	RequestID      string   `json:"request_id"`
	Sha256         string   `json:"sha256"`
	Created        string   `json:"created"`
	Duration       float64  `json:"duration"`
	Channels       int      `json:"channels"`
	Models         []string `json:"models"`
}

type deepgramResults struct {
	Channels []deepgramChannel `json:"channels"`
}

type deepgramChannel struct {
	Alternatives     []deepgramAlternative `json:"alternatives"`
	DetectedLanguage string                `json:"detected_language,omitempty"`
}

type deepgramAlternative struct {
	Transcript string         `json:"transcript"`
	Confidence float64        `json:"confidence"`
	Words      []deepgramWord `json:"words"`
}

type deepgramWord struct {
	Word          string  `json:"word"`
	Start         float64 `json:"start"`
	End           float64 `json:"end"`
	Confidence    float64 `json:"confidence"`
	Speaker       *int    `json:"speaker,omitempty"`
	SpeakerChange bool    `json:"speaker_change,omitempty"`
}

type errorResponse struct {
	ErrCode string `json:"err_code"`
	ErrMsg  string `json:"err_msg"`
}
