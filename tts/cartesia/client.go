// Package cartesia provides a Cartesia TTS provider implementation.
package cartesia

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/shillcollin/gai/tts"
)

const (
	defaultBaseURL    = "https://api.cartesia.ai"
	defaultModel      = "sonic-3"
	defaultVoice      = "a0e99841-438c-4a64-b679-ae501e7d6091" // Default Cartesia voice
	defaultAPIVersion = "2025-04-16"
)

// Well-known voice IDs for convenience
var WellKnownVoices = map[string]string{
	"barbershop-man":       "a0e99841-438c-4a64-b679-ae501e7d6091",
	"british-lady":         "79a125e8-cd45-4c13-8a67-188112f4dd22",
	"child":                "2ee87190-8f84-4925-97da-e52547f9462c",
	"classy-british-man":   "95856005-0332-41b0-935f-352e296aa0df",
	"commercial-lady":      "c2ac25f9-ecc4-4f56-9095-651354df60c0",
	"commercial-man":       "7360f116-6306-4e9a-b487-1235f35a0f21",
	"confident-woman":      "b7d50908-b17c-442d-ad8d-810c63997ed9",
	"doctor-mischief":      "fb26447f-308b-471e-8b00-8e9f04284eb5",
	"female-nurse":         "5c42302c-194b-4d0c-ba1a-8cb485c84ab9",
	"friendly-reading-man": "69267136-1bdc-412f-ad78-0caad210fb40",
	"hannah":               "8985388c-1332-4ce7-8d55-789628aa3df4",
	"indian-lady":          "03496517-369a-4db1-8236-3d3ae459ddf7",
	"indian-man":           "c45bc5ec-dc68-4feb-8829-6e6b2748095d",
	"laidback-woman":       "21b81c14-f85b-436d-aff5-43f2e788ecf8",
	"male-nurse":           "a167e0f3-df7e-4d52-a9c3-f949145efdab",
	"maria":                "5619d38c-cf51-4d8e-9575-48f61a280571",
	"midwestern-woman":     "11af83e2-23eb-452f-956e-7fee218ccb5c",
	"movieman":             "c45bc5d0-e6f5-4a05-8f16-c3e0a8e0b7a6",
	"narrator-lady":        "5f6ad391-4c30-4d8e-8c29-2a6e78b85c95",
	"newsman":              "d46abd1d-2f52-4c3e-8e96-c33e7c10b946",
	"nonfiction-man":       "79f8b5fb-2cc8-479a-80df-29f7a7cf1a3e",
	"pilot-over-intercom":  "4d2fd738-3b3d-4c94-a350-2e7dfb1c4893",
	"polite-man":           "ee7ea9f8-c0c1-498c-9f62-dc2da49e7e11",
	"princess":             "c47f9f4a-3d87-4d3d-b5c1-f3f6f2e0b9c1",
	"professor":            "e00d0e4c-a5c8-4f26-a55a-8cc5df14eb0a",
	"reading-lady":         "15a9cd88-84b0-4a8b-95f2-5d583b54c72e",
	"reflective-woman":     "a3520a8f-226a-428d-9fcd-b0a4711a6829",
	"reporter-man":         "8e40b0d9-57a2-4e36-a8b0-58b4f3c4ad93",
	"salesman":             "820a3788-2b37-4d21-847a-b65d8a68c99a",
	"sarah":                "a8a1eb38-5f15-4c1d-8722-7ac0f329727d",
	"scientist":            "63867a0d-6058-43b9-8b97-c1a9fe59d3a7",
	"southern-belle":       "f9836c6e-a0bd-460e-9d3c-f7a94f5bbf75",
	"southern-man":         "98a34ef2-2140-4c28-9c71-663dc4dd7022",
	"spanish-narrator":     "846d6cb0-2301-48b6-9571-6d4e0c91f8e3",
	"sportscaster":         "a167e0f3-df7e-4d52-a9c3-f949145efdab",
	"storyteller-lady":     "996a8b96-4804-46c7-8e0b-4fe3d4c0b0a8",
	"sweet-lady":           "e3827ec5-697a-4b7c-9188-d4b7c31bb2a6",
	"teacher-lady":         "573e3144-a684-4e72-ac2b-9b2063a50b53",
	"tutorial-man":         "bd9120b6-7761-47a6-a446-77ca49132781",
	"wise-guide-man":       "42b39f37-515f-4eee-8546-73e841679c1d",
	"wise-lady":            "c8605446-247c-4d39-acd4-8f4c28aa363c",
	"young-professional":   "248be419-c632-4f23-adf1-5324ed7dbf1d",
}

// Client is a Cartesia TTS provider.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	model      string
	voice      string
	apiVersion string
}

// Option configures a Cartesia client.
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

// WithVoice sets the default voice.
func WithVoice(voice string) Option {
	return func(c *Client) {
		c.voice = voice
	}
}

// WithAPIVersion sets the API version.
func WithAPIVersion(version string) Option {
	return func(c *Client) {
		c.apiVersion = version
	}
}

// New creates a new Cartesia client.
func New(opts ...Option) *Client {
	c := &Client{
		baseURL:    defaultBaseURL,
		httpClient: http.DefaultClient,
		model:      defaultModel,
		voice:      defaultVoice,
		apiVersion: defaultAPIVersion,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Synthesize implements tts.Provider.
func (c *Client) Synthesize(ctx context.Context, text string, opts tts.Options) (*tts.Audio, error) {
	// Resolve voice ID
	voiceID := c.resolveVoice(opts.Voice)

	// Build request body
	body := synthesizeRequest{
		ModelID:    c.resolveModel(opts.Model),
		Transcript: text,
		Voice: voiceConfig{
			Mode: "id",
			ID:   voiceID,
		},
		OutputFormat: c.buildOutputFormat(opts),
		Language:     "en",
	}

	// Add generation config if speed is specified
	if opts.Speed != 0 {
		body.GenerationConfig = &generationConfig{
			Speed: opts.Speed,
		}
	}

	// Serialize request
	reqBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("cartesia: marshal request: %w", err)
	}

	// Use SSE endpoint for streaming audio, collect into bytes
	reqURL := fmt.Sprintf("%s/tts/sse", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("cartesia: create request: %w", err)
	}

	// Set headers
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Cartesia-Version", c.apiVersion)
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cartesia: request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check for errors
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cartesia: unexpected status %d: %s", resp.StatusCode, string(data))
	}

	// Parse SSE stream and collect audio chunks
	audioData, err := c.collectSSEAudio(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cartesia: collect audio: %w", err)
	}

	format, sampleRate := c.parseOutputFormat(opts)

	return &tts.Audio{
		Data:       audioData,
		Format:     format,
		SampleRate: sampleRate,
		Voice:      voiceID,
		Model:      body.ModelID,
		Provider:   "cartesia",
	}, nil
}

// StreamSynthesize implements tts.Provider.
func (c *Client) StreamSynthesize(ctx context.Context, textChan <-chan string, opts tts.Options) (*tts.AudioStream, error) {
	voiceID := c.resolveVoice(opts.Voice)
	format, sampleRate := c.parseOutputFormat(opts)

	stream := tts.NewAudioStream(&tts.AudioMeta{
		Format:     format,
		SampleRate: sampleRate,
		Voice:      voiceID,
		Model:      c.resolveModel(opts.Model),
		Provider:   "cartesia",
	})

	go func() {
		defer stream.Close()

		// Collect all text first (simplified streaming implementation)
		var allText strings.Builder
		for text := range textChan {
			allText.WriteString(text)
		}

		if allText.Len() == 0 {
			return
		}

		// Build request
		body := synthesizeRequest{
			ModelID:    c.resolveModel(opts.Model),
			Transcript: allText.String(),
			Voice: voiceConfig{
				Mode: "id",
				ID:   voiceID,
			},
			OutputFormat: c.buildOutputFormat(opts),
			Language:     "en",
		}

		if opts.Speed != 0 {
			body.GenerationConfig = &generationConfig{
				Speed: opts.Speed,
			}
		}

		reqBody, err := json.Marshal(body)
		if err != nil {
			stream.SetError(err)
			stream.Send(tts.AudioEvent{
				Type:      tts.AudioEventError,
				Error:     err,
				Timestamp: time.Now(),
			})
			return
		}

		reqURL := fmt.Sprintf("%s/tts/sse", c.baseURL)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(reqBody))
		if err != nil {
			stream.SetError(err)
			stream.Send(tts.AudioEvent{
				Type:      tts.AudioEventError,
				Error:     err,
				Timestamp: time.Now(),
			})
			return
		}

		req.Header.Set("X-API-Key", c.apiKey)
		req.Header.Set("Cartesia-Version", c.apiVersion)
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			stream.SetError(err)
			stream.Send(tts.AudioEvent{
				Type:      tts.AudioEventError,
				Error:     err,
				Timestamp: time.Now(),
			})
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			data, _ := io.ReadAll(resp.Body)
			err := fmt.Errorf("cartesia: unexpected status %d: %s", resp.StatusCode, string(data))
			stream.SetError(err)
			stream.Send(tts.AudioEvent{
				Type:      tts.AudioEventError,
				Error:     err,
				Timestamp: time.Now(),
			})
			return
		}

		// Stream SSE events
		if err := c.streamSSEAudio(resp.Body, stream); err != nil {
			stream.SetError(err)
			stream.Send(tts.AudioEvent{
				Type:      tts.AudioEventError,
				Error:     err,
				Timestamp: time.Now(),
			})
		}
	}()

	return stream, nil
}

// Voices implements tts.Provider.
func (c *Client) Voices() []tts.Voice {
	voices := make([]tts.Voice, 0, len(WellKnownVoices))
	for name, id := range WellKnownVoices {
		voices = append(voices, tts.Voice{
			ID:       id,
			Name:     name,
			Language: "en",
			Gender:   inferGender(name),
		})
	}
	return voices
}

// Capabilities implements tts.Provider.
func (c *Client) Capabilities() tts.Capabilities {
	return tts.Capabilities{
		Provider:  "cartesia",
		Voices:    c.Voices(),
		Languages: []string{"en", "fr", "de", "es", "pt", "zh", "ja", "hi", "it", "ko", "nl", "pl", "ru", "sv", "tr"},
		Realtime:  true,
		SSML:      false,
		Cloning:   false,
	}
}

// resolveVoice resolves a voice name to a voice ID.
func (c *Client) resolveVoice(voice string) string {
	if voice == "" {
		return c.voice
	}
	// Check if it's a well-known voice name
	if id, ok := WellKnownVoices[strings.ToLower(voice)]; ok {
		return id
	}
	// Assume it's already a voice ID
	return voice
}

// resolveModel resolves the model ID.
func (c *Client) resolveModel(model string) string {
	if model == "" {
		return c.model
	}
	return model
}

// buildOutputFormat builds the output format configuration.
func (c *Client) buildOutputFormat(opts tts.Options) outputFormat {
	sampleRate := opts.SampleRate
	if sampleRate == 0 {
		sampleRate = 24000
	}

	switch opts.Format {
	case tts.FormatPCM, tts.FormatWAV:
		return outputFormat{
			Container:  "raw",
			Encoding:   "pcm_s16le",
			SampleRate: sampleRate,
		}
	case tts.FormatMP3:
		return outputFormat{
			Container:  "mp3",
			Encoding:   "mp3",
			SampleRate: sampleRate,
		}
	default:
		// Default to raw PCM for low latency
		return outputFormat{
			Container:  "raw",
			Encoding:   "pcm_s16le",
			SampleRate: sampleRate,
		}
	}
}

// parseOutputFormat returns the format and sample rate from options.
func (c *Client) parseOutputFormat(opts tts.Options) (tts.AudioFormat, int) {
	sampleRate := opts.SampleRate
	if sampleRate == 0 {
		sampleRate = 24000
	}

	switch opts.Format {
	case tts.FormatMP3:
		return tts.FormatMP3, sampleRate
	case tts.FormatWAV:
		return tts.FormatWAV, sampleRate
	default:
		return tts.FormatPCM, sampleRate
	}
}

// collectSSEAudio reads SSE events and collects audio data.
func (c *Client) collectSSEAudio(r io.Reader) ([]byte, error) {
	var audioData []byte
	decoder := json.NewDecoder(r)

	for {
		var event sseEvent
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			// Try reading raw line for SSE format
			break
		}

		if event.Type == "chunk" && event.Data != "" {
			chunk, err := base64.StdEncoding.DecodeString(event.Data)
			if err != nil {
				return nil, fmt.Errorf("decode audio chunk: %w", err)
			}
			audioData = append(audioData, chunk...)
		}

		if event.Done {
			break
		}

		if event.Error != "" {
			return nil, fmt.Errorf("cartesia error: %s", event.Error)
		}
	}

	return audioData, nil
}

// streamSSEAudio reads SSE events and streams audio data.
func (c *Client) streamSSEAudio(r io.Reader, stream *tts.AudioStream) error {
	decoder := json.NewDecoder(r)

	for {
		var event sseEvent
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			break
		}

		if event.Type == "chunk" && event.Data != "" {
			chunk, err := base64.StdEncoding.DecodeString(event.Data)
			if err != nil {
				return fmt.Errorf("decode audio chunk: %w", err)
			}
			stream.Send(tts.AudioEvent{
				Type:      tts.AudioEventDelta,
				Data:      chunk,
				Timestamp: time.Now(),
			})
		}

		if event.Done {
			stream.Send(tts.AudioEvent{
				Type:      tts.AudioEventFinish,
				Timestamp: time.Now(),
			})
			break
		}

		if event.Error != "" {
			return fmt.Errorf("cartesia error: %s", event.Error)
		}
	}

	return nil
}

// inferGender infers gender from voice name (heuristic).
func inferGender(name string) string {
	femaleKeywords := []string{"lady", "woman", "belle", "girl", "princess", "nurse", "female", "hannah", "maria", "sarah", "sweet"}
	maleKeywords := []string{"man", "guy", "male", "professor", "doctor", "pilot", "newsman", "sportscaster"}

	lower := strings.ToLower(name)
	for _, kw := range femaleKeywords {
		if strings.Contains(lower, kw) {
			return "female"
		}
	}
	for _, kw := range maleKeywords {
		if strings.Contains(lower, kw) {
			return "male"
		}
	}
	return "neutral"
}

// Request/response types

type synthesizeRequest struct {
	ModelID          string            `json:"model_id"`
	Transcript       string            `json:"transcript"`
	Voice            voiceConfig       `json:"voice"`
	OutputFormat     outputFormat      `json:"output_format"`
	Language         string            `json:"language,omitempty"`
	GenerationConfig *generationConfig `json:"generation_config,omitempty"`
}

type voiceConfig struct {
	Mode string `json:"mode"`
	ID   string `json:"id"`
}

type outputFormat struct {
	Container  string `json:"container"`
	Encoding   string `json:"encoding"`
	SampleRate int    `json:"sample_rate"`
}

type generationConfig struct {
	Speed   float64 `json:"speed,omitempty"`
	Volume  float64 `json:"volume,omitempty"`
	Emotion string  `json:"emotion,omitempty"`
}

type sseEvent struct {
	Type       string `json:"type"`
	Data       string `json:"data,omitempty"`
	Done       bool   `json:"done"`
	Error      string `json:"error,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
	ContextID  string `json:"context_id,omitempty"`
}
