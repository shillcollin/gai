// Package elevenlabs provides an ElevenLabs TTS provider implementation.
package elevenlabs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/shillcollin/gai/tts"
)

const (
	defaultBaseURL = "https://api.elevenlabs.io"
	defaultModel   = "eleven_multilingual_v2"
	defaultVoice   = "21m00Tcm4TlvDq8ikWAM" // Rachel
)

// Well-known voice IDs for convenience
var WellKnownVoices = map[string]string{
	"rachel":  "21m00Tcm4TlvDq8ikWAM",
	"adam":    "pNInz6obpgDQGcFmaJgB",
	"antoni":  "ErXwobaYiN019PkySvjV",
	"bella":   "EXAVITQu4vr4xnSDxMaL",
	"elli":    "MF3mGyEYCl7XYWbV9V6O",
	"josh":    "TxGEqnHWrfWFTfGW9XjX",
	"arnold":  "VR6AewLTigWG4xSOukaG",
	"domi":    "AZnzlk1XvdvUeBnXmlld",
	"sam":     "yoZ06aMxZJJ28mfd3POQ",
	"emily":   "LcfcDJNUP1GQjkzn1xUU",
	"callum":  "N2lVS1w4EtoT3dr4eOWO",
	"charlie": "IKne3meq5aSn9XLyUdCD",
	"clyde":   "2EiwWnXFnvU5JabPnv8n",
	"daniel":  "onwK4e9ZLuTAKqWW03F9",
	"dave":    "CYw3kZ02Hs0563khs1Fj",
	"dorothy": "ThT5KcBeYPX3keUQqHPh",
	"fin":     "D38z5RcWu1voky8WS1ja",
	"freya":   "jsCqWAovK2LkecY7zXl4",
	"george":  "JBFqnCBsd6RMkjVDRZzb",
	"gigi":    "jBpfuIE2acCO8z3wKNLl",
	"giovanni": "zcAOhNBS3c14rBihAFp1",
	"glinda":  "z9fAnlkpzviPz146aGWa",
	"grace":   "oWAxZDx7w5VEj9dCyTzz",
	"harry":   "SOYHLrjzK2X1ezoPC6cr",
	"james":   "ZQe5CZNOzWyzPSCn5a3c",
	"jeremy":  "bVMeCyTHy58xNoL34h3p",
	"jessie":  "t0jbNlBVZ17f02VDIeMI",
	"joseph":  "Zlb1dXrM653N07WRdFW3",
	"liam":    "TX3LPaxmHKxFdv7VOQHJ",
	"lily":    "pFZP5JQG7iQjIQuC4Bku",
	"matilda": "XrExE9yKIg1WjnnlVkGX",
	"michael": "flq6f7yk4E4fJM5XTYuZ",
	"mimi":    "zrHiDhphv9ZnVXBqCLjz",
	"nicole":  "piTKgcLEGmPE4e6mEKli",
	"patrick": "ODq5zmih8GrVes37Dizd",
	"paul":    "5Q0t7uMcjvnagumLfvZi",
	"river":   "SAz9YHcvj6GT2YYXdXww",
	"roger":   "CwhRBWXzGAHq8TQ4Fs17",
	"sarah":   "EXAVITQu4vr4xnSDxMaL",
	"serena":  "pMsXgVXv3BLzUgSXRplE",
	"thomas":  "GBv7mTt0atIp3Br8iCZE",
	"bill":    "pqHfZKP75CvOlQylNhV4",
	"brian":   "nPczCjzI2devNBz1zQrb",
	"chris":   "iP95p4xoKVk53GoZ742B",
}

// Client is an ElevenLabs TTS provider.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	model      string
	voice      string
}

// Option configures an ElevenLabs client.
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

// New creates a new ElevenLabs client.
func New(opts ...Option) *Client {
	c := &Client{
		baseURL:    defaultBaseURL,
		httpClient: http.DefaultClient,
		model:      defaultModel,
		voice:      defaultVoice,
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
		Text:    text,
		ModelID: c.resolveModel(opts.Model),
	}

	// Add voice settings if provided
	if opts.Speed != 0 {
		body.VoiceSettings = &voiceSettings{
			Speed: opts.Speed,
		}
	}

	// Serialize request
	reqBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs: marshal request: %w", err)
	}

	// Build URL with output format
	format := c.resolveFormat(opts.Format)
	reqURL := fmt.Sprintf("%s/v1/text-to-speech/%s?output_format=%s", c.baseURL, voiceID, format)

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("elevenlabs: create request: %w", err)
	}

	// Set headers
	req.Header.Set("xi-api-key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs: request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs: read response: %w", err)
	}

	// Check for errors
	if resp.StatusCode != http.StatusOK {
		var errResp errorResponse
		if json.Unmarshal(data, &errResp) == nil && errResp.Detail.Message != "" {
			return nil, fmt.Errorf("elevenlabs: %s", errResp.Detail.Message)
		}
		return nil, fmt.Errorf("elevenlabs: unexpected status %d: %s", resp.StatusCode, string(data))
	}

	// Parse format for metadata
	audioFormat, sampleRate := parseFormat(format)

	return &tts.Audio{
		Data:       data,
		Format:     audioFormat,
		SampleRate: sampleRate,
		Voice:      voiceID,
		Model:      body.ModelID,
		Provider:   "elevenlabs",
	}, nil
}

// StreamSynthesize implements tts.Provider.
func (c *Client) StreamSynthesize(ctx context.Context, textChan <-chan string, opts tts.Options) (*tts.AudioStream, error) {
	// Resolve voice ID
	voiceID := c.resolveVoice(opts.Voice)
	format := c.resolveFormat(opts.Format)
	audioFormat, sampleRate := parseFormat(format)

	stream := tts.NewAudioStream(&tts.AudioMeta{
		Format:     audioFormat,
		SampleRate: sampleRate,
		Voice:      voiceID,
		Model:      c.resolveModel(opts.Model),
		Provider:   "elevenlabs",
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

		// Build request body
		body := synthesizeRequest{
			Text:    allText.String(),
			ModelID: c.resolveModel(opts.Model),
		}

		if opts.Speed != 0 {
			body.VoiceSettings = &voiceSettings{
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

		// Use streaming endpoint
		reqURL := fmt.Sprintf("%s/v1/text-to-speech/%s/stream?output_format=%s", c.baseURL, voiceID, format)

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

		req.Header.Set("xi-api-key", c.apiKey)
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
			err := fmt.Errorf("elevenlabs: unexpected status %d: %s", resp.StatusCode, string(data))
			stream.SetError(err)
			stream.Send(tts.AudioEvent{
				Type:      tts.AudioEventError,
				Error:     err,
				Timestamp: time.Now(),
			})
			return
		}

		// Stream audio chunks
		buf := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				stream.Send(tts.AudioEvent{
					Type:      tts.AudioEventDelta,
					Data:      chunk,
					Timestamp: time.Now(),
				})
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				stream.SetError(err)
				stream.Send(tts.AudioEvent{
					Type:      tts.AudioEventError,
					Error:     err,
					Timestamp: time.Now(),
				})
				return
			}
		}

		stream.Send(tts.AudioEvent{
			Type:      tts.AudioEventFinish,
			Timestamp: time.Now(),
		})
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
		Provider:  "elevenlabs",
		Voices:    c.Voices(),
		Languages: []string{"en", "es", "fr", "de", "it", "pt", "pl", "hi", "ar", "zh", "ja", "ko"},
		Realtime:  true,
		SSML:      false,
		Cloning:   true,
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

// resolveFormat resolves the output format string.
func (c *Client) resolveFormat(format tts.AudioFormat) string {
	switch format {
	case tts.FormatMP3:
		return "mp3_44100_128"
	case tts.FormatPCM:
		return "pcm_24000"
	case tts.FormatWAV:
		return "pcm_24000" // ElevenLabs returns raw PCM, client can wrap in WAV
	case tts.FormatOpus:
		return "opus_48000_128"
	default:
		return "mp3_44100_128"
	}
}

// parseFormat parses an ElevenLabs format string into AudioFormat and sample rate.
func parseFormat(format string) (tts.AudioFormat, int) {
	switch {
	case strings.HasPrefix(format, "mp3"):
		parts := strings.Split(format, "_")
		if len(parts) >= 2 {
			var rate int
			fmt.Sscanf(parts[1], "%d", &rate)
			return tts.FormatMP3, rate
		}
		return tts.FormatMP3, 44100
	case strings.HasPrefix(format, "pcm"):
		parts := strings.Split(format, "_")
		if len(parts) >= 2 {
			var rate int
			fmt.Sscanf(parts[1], "%d", &rate)
			return tts.FormatPCM, rate
		}
		return tts.FormatPCM, 24000
	case strings.HasPrefix(format, "opus"):
		parts := strings.Split(format, "_")
		if len(parts) >= 2 {
			var rate int
			fmt.Sscanf(parts[1], "%d", &rate)
			return tts.FormatOpus, rate
		}
		return tts.FormatOpus, 48000
	default:
		return tts.FormatMP3, 44100
	}
}

// inferGender infers gender from voice name (heuristic).
func inferGender(name string) string {
	femaleNames := map[string]bool{
		"rachel": true, "bella": true, "elli": true, "domi": true,
		"emily": true, "dorothy": true, "freya": true, "gigi": true,
		"glinda": true, "grace": true, "jessie": true, "lily": true,
		"matilda": true, "mimi": true, "nicole": true, "sarah": true,
		"serena": true,
	}
	if femaleNames[strings.ToLower(name)] {
		return "female"
	}
	return "male"
}

// Request/response types

type synthesizeRequest struct {
	Text          string         `json:"text"`
	ModelID       string         `json:"model_id,omitempty"`
	VoiceSettings *voiceSettings `json:"voice_settings,omitempty"`
}

type voiceSettings struct {
	Stability       float64 `json:"stability,omitempty"`
	SimilarityBoost float64 `json:"similarity_boost,omitempty"`
	Style           float64 `json:"style,omitempty"`
	UseSpeakerBoost bool    `json:"use_speaker_boost,omitempty"`
	Speed           float64 `json:"speed,omitempty"`
}

type errorResponse struct {
	Detail struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	} `json:"detail"`
}
