package gai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/shillcollin/gai/core"
)

// Conversation provides automatic history management for multi-turn interactions.
// It wraps a Client and maintains message history across calls.
//
// Example:
//
//	conv := client.Conversation(
//	    gai.ConvModel("anthropic/claude-3-5-sonnet"),
//	    gai.ConvSystem("You are a helpful assistant"),
//	)
//
//	reply, _ := conv.Say(ctx, "Hello!")
//	fmt.Println(reply.Text())
//
//	reply, _ = conv.Say(ctx, "What did I just say?")
//	fmt.Println(reply.Text()) // Remembers context
type Conversation struct {
	mu       sync.RWMutex
	client   *Client
	messages []core.Message
	model    string
	system   string
	voice    string
	stt      string
	tools    []core.ToolHandle
	maxMsgs  int            // Max messages to keep (0 = unlimited)
	metadata map[string]any
}

// AudioInput wraps audio bytes for use as conversation input.
type AudioInput struct {
	Data []byte
	MIME string
}

// ConvAudio creates an AudioInput from raw bytes with default MIME type.
func ConvAudio(data []byte) AudioInput {
	return AudioInput{Data: data, MIME: "audio/wav"}
}

// ConvAudioWithMIME creates an AudioInput with a specific MIME type.
func ConvAudioWithMIME(data []byte, mime string) AudioInput {
	return AudioInput{Data: data, MIME: mime}
}

// Say sends a message and returns the response, automatically managing history.
// Input can be: string, AudioInput, []byte (treated as audio), or core.Message.
//
// Example:
//
//	// Text input
//	reply, err := conv.Say(ctx, "Hello!")
//
//	// Audio input (with voice config)
//	reply, err := conv.Say(ctx, gai.ConvAudio(speechBytes))
//	fmt.Println("You said:", reply.Transcript())
//	fmt.Println("Response:", reply.Text())
func (c *Conversation) Say(ctx context.Context, input any, opts ...CallOption) (*Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.sayLocked(ctx, input, opts...)
}

// sayLocked is the internal implementation of Say (caller must hold lock).
func (c *Conversation) sayLocked(ctx context.Context, input any, opts ...CallOption) (*Result, error) {
	// Convert input to message
	userMsg, err := c.inputToMessage(input)
	if err != nil {
		return nil, err
	}

	// Apply call options
	cfg := c.buildCallConfig(opts...)

	// Build request
	req := c.buildRequest(userMsg, cfg)

	// Generate response
	result, err := c.client.Generate(ctx, req)
	if err != nil {
		return nil, err
	}

	// Update history
	c.appendToHistory(userMsg, result)

	return result, nil
}

// Stream sends a message and streams the response.
// History is updated after the stream completes.
//
// Example:
//
//	stream, err := conv.Stream(ctx, "Tell me a story")
//	if err != nil {
//	    return err
//	}
//	defer stream.Close()
//
//	for event := range stream.Events() {
//	    if event.Type == core.EventTextDelta {
//	        fmt.Print(event.TextDelta)
//	    }
//	}
func (c *Conversation) Stream(ctx context.Context, input any, opts ...CallOption) (*ConversationStream, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Convert input to message
	userMsg, err := c.inputToMessage(input)
	if err != nil {
		return nil, err
	}

	// Apply call options
	cfg := c.buildCallConfig(opts...)

	// Build request
	req := c.buildRequest(userMsg, cfg)

	// Start streaming
	stream, err := c.client.Stream(ctx, req)
	if err != nil {
		return nil, err
	}

	return &ConversationStream{
		stream:  stream,
		conv:    c,
		userMsg: userMsg,
	}, nil
}

// Run executes an agentic loop with tools, managing history automatically.
//
// Example:
//
//	conv := client.Conversation(
//	    gai.ConvModel("anthropic/claude-3-5-sonnet"),
//	    gai.ConvTools(searchTool, calcTool),
//	)
//
//	result, err := conv.Run(ctx, "Search for the population of France")
func (c *Conversation) Run(ctx context.Context, input any, opts ...CallOption) (*Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Convert input to message
	userMsg, err := c.inputToMessage(input)
	if err != nil {
		return nil, err
	}

	// Apply call options
	cfg := c.buildCallConfig(opts...)

	// Build request (will use tools)
	req := c.buildRequest(userMsg, cfg)

	// Run agentic loop
	result, err := c.client.Run(ctx, req)
	if err != nil {
		return nil, err
	}

	// Update history
	c.appendToHistory(userMsg, result)

	return result, nil
}

// StreamRun executes an agentic loop while streaming events.
//
// Example:
//
//	stream, err := conv.StreamRun(ctx, "Research AI news")
//	for event := range stream.Events() {
//	    switch event.Type {
//	    case core.EventTextDelta:
//	        fmt.Print(event.TextDelta)
//	    case core.EventToolCall:
//	        fmt.Printf("\n[Calling %s]\n", event.ToolCall.Name)
//	    }
//	}
func (c *Conversation) StreamRun(ctx context.Context, input any, opts ...CallOption) (*ConversationStream, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Convert input to message
	userMsg, err := c.inputToMessage(input)
	if err != nil {
		return nil, err
	}

	// Apply call options
	cfg := c.buildCallConfig(opts...)

	// Build request
	req := c.buildRequest(userMsg, cfg)

	// Start streaming run
	stream, err := c.client.StreamRun(ctx, req)
	if err != nil {
		return nil, err
	}

	return &ConversationStream{
		stream:  stream,
		conv:    c,
		userMsg: userMsg,
	}, nil
}

// Messages returns a copy of the conversation history.
func (c *Conversation) Messages() []core.Message {
	c.mu.RLock()
	defer c.mu.RUnlock()

	msgs := make([]core.Message, len(c.messages))
	copy(msgs, c.messages)
	return msgs
}

// AddMessages appends messages to the conversation history.
func (c *Conversation) AddMessages(msgs ...core.Message) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.messages = append(c.messages, msgs...)
	c.trim()
}

// Clear removes all messages from the conversation history.
func (c *Conversation) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.messages = c.messages[:0]
}

// Rollback removes the last n messages from history.
func (c *Conversation) Rollback(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if n >= len(c.messages) {
		c.messages = c.messages[:0]
		return
	}
	c.messages = c.messages[:len(c.messages)-n]
}

// Fork creates an independent copy of the conversation.
// Changes to the fork don't affect the original.
func (c *Conversation) Fork() *Conversation {
	c.mu.RLock()
	defer c.mu.RUnlock()

	msgs := make([]core.Message, len(c.messages))
	copy(msgs, c.messages)

	tools := make([]core.ToolHandle, len(c.tools))
	copy(tools, c.tools)

	metadata := make(map[string]any, len(c.metadata))
	for k, v := range c.metadata {
		metadata[k] = v
	}

	return &Conversation{
		client:   c.client,
		messages: msgs,
		model:    c.model,
		system:   c.system,
		voice:    c.voice,
		stt:      c.stt,
		tools:    tools,
		maxMsgs:  c.maxMsgs,
		metadata: metadata,
	}
}

// Model returns the configured model.
func (c *Conversation) Model() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.model
}

// System returns the system prompt.
func (c *Conversation) System() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.system
}

// MessageCount returns the number of messages in history.
func (c *Conversation) MessageCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.messages)
}

// inputToMessage converts the input to a core.Message.
func (c *Conversation) inputToMessage(input any) (core.Message, error) {
	switch v := input.(type) {
	case string:
		return core.UserMessage(core.Text{Text: v}), nil
	case AudioInput:
		return core.UserMessage(core.Audio{
			Source: core.BlobRef{
				Kind:  core.BlobBytes,
				Bytes: v.Data,
				MIME:  v.MIME,
				Size:  int64(len(v.Data)),
			},
		}), nil
	case []byte:
		return core.UserMessage(core.Audio{
			Source: core.BlobRef{
				Kind:  core.BlobBytes,
				Bytes: v,
				MIME:  "audio/wav",
				Size:  int64(len(v)),
			},
		}), nil
	case core.Message:
		return v, nil
	default:
		return core.Message{}, fmt.Errorf("unsupported input type: %T (expected string, AudioInput, []byte, or core.Message)", input)
	}
}

// callConfig holds per-call options.
type callConfig struct {
	voice string
	tools []core.ToolHandle
}

// buildCallConfig applies call options and returns the config.
func (c *Conversation) buildCallConfig(opts ...CallOption) callConfig {
	cfg := callConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// buildRequest constructs a RequestBuilder for the given user message.
func (c *Conversation) buildRequest(userMsg core.Message, cfg callConfig) *RequestBuilder {
	req := Request(c.model)

	// Add system message first if we have one and no messages yet
	if c.system != "" {
		// Check if system message is already in history
		hasSystem := false
		for _, msg := range c.messages {
			if msg.Role == core.System {
				hasSystem = true
				break
			}
		}
		if !hasSystem {
			req = req.System(c.system)
		}
	}

	// Add history
	req = req.Messages(c.messages...)

	// Add new user message
	req = req.Messages(userMsg)

	// Add tools (prefer call-specific, fall back to conversation-level)
	tools := c.tools
	if len(cfg.tools) > 0 {
		tools = cfg.tools
	}
	if len(tools) > 0 {
		req = req.Tools(tools...)
	}

	// Add voice (prefer call-specific, fall back to conversation-level)
	voice := c.voice
	if cfg.voice != "" {
		voice = cfg.voice
	}
	if voice != "" {
		req = req.Voice(voice)
	}

	// Add STT
	if c.stt != "" {
		req = req.STT(c.stt)
	}

	return req
}

// appendToHistory adds the user message and result messages to history.
func (c *Conversation) appendToHistory(userMsg core.Message, result *Result) {
	c.messages = append(c.messages, userMsg)
	c.messages = append(c.messages, result.Messages()...)
	c.trim()
}

// trim enforces the maxMsgs limit.
func (c *Conversation) trim() {
	if c.maxMsgs <= 0 || len(c.messages) <= c.maxMsgs {
		return
	}

	// Find where to start keeping messages
	startIdx := len(c.messages) - c.maxMsgs

	// If first message is system, preserve it
	if len(c.messages) > 0 && c.messages[0].Role == core.System {
		// Keep system message + last (maxMsgs-1) messages
		if startIdx < 1 {
			startIdx = 1
		}
		preserved := make([]core.Message, 0, c.maxMsgs)
		preserved = append(preserved, c.messages[0])
		preserved = append(preserved, c.messages[startIdx:]...)
		c.messages = preserved
		return
	}

	c.messages = c.messages[startIdx:]
}

// ConversationStream wraps a Stream and updates conversation history on completion.
type ConversationStream struct {
	stream  *Stream
	conv    *Conversation
	userMsg core.Message

	// Track collected text as events are consumed
	mu            sync.Mutex
	collectedText string
	result        *Result
	closed        bool
}

// Events returns a channel that wraps the underlying stream events,
// tracking collected text for history updates.
func (cs *ConversationStream) Events() <-chan core.StreamEvent {
	// Create a wrapper channel that tracks text as it streams
	out := make(chan core.StreamEvent)
	go func() {
		defer close(out)
		for event := range cs.stream.Events() {
			if event.Type == core.EventTextDelta {
				cs.mu.Lock()
				cs.collectedText += event.TextDelta
				cs.mu.Unlock()
			}
			out <- event
		}
	}()
	return out
}

// Result returns the final result after streaming completes.
func (cs *ConversationStream) Result() *Result {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if cs.result == nil {
		cs.result = &Result{
			inner: &core.TextResult{
				Text: cs.collectedText,
			},
		}
	}
	return cs.result
}

// Close closes the stream and updates conversation history.
func (cs *ConversationStream) Close() error {
	cs.mu.Lock()
	if cs.closed {
		cs.mu.Unlock()
		return nil
	}
	cs.closed = true

	// Build result from collected text
	result := &Result{
		inner: &core.TextResult{
			Text: cs.collectedText,
		},
	}
	cs.result = result
	cs.mu.Unlock()

	// Update conversation history
	cs.conv.mu.Lock()
	cs.conv.appendToHistory(cs.userMsg, result)
	cs.conv.mu.Unlock()

	return cs.stream.Close()
}

// Err returns any error from the stream.
func (cs *ConversationStream) Err() error {
	return cs.stream.Err()
}

// MarshalJSON serializes the conversation to JSON.
func (c *Conversation) MarshalJSON() ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	data := conversationJSON{
		Model:    c.model,
		System:   c.system,
		Voice:    c.voice,
		STT:      c.stt,
		MaxMsgs:  c.maxMsgs,
		Metadata: c.metadata,
		Messages: make([]messageJSON, 0, len(c.messages)),
	}

	for _, msg := range c.messages {
		msgJSON := messageJSON{
			Role:  string(msg.Role),
			Parts: make([]partJSON, 0, len(msg.Parts)),
		}

		for _, part := range msg.Parts {
			pj := partJSON{}
			switch p := part.(type) {
			case core.Text:
				pj.Type = "text"
				pj.Text = p.Text
			case core.Audio:
				pj.Type = "audio"
				if p.Source.Kind == core.BlobBytes {
					pj.AudioBase64 = base64.StdEncoding.EncodeToString(p.Source.Bytes)
					pj.AudioMIME = p.Source.MIME
				}
			case core.Image:
				pj.Type = "image"
				if p.Source.Kind == core.BlobBytes {
					pj.ImageBase64 = base64.StdEncoding.EncodeToString(p.Source.Bytes)
					pj.ImageMIME = p.Source.MIME
				}
			case core.ImageURL:
				pj.Type = "image_url"
				pj.ImageURL = p.URL
			case core.ToolCall:
				pj.Type = "tool_call"
				pj.ToolCallID = p.ID
				pj.ToolCallName = p.Name
				pj.ToolCallInput = p.Input
			case core.ToolResult:
				pj.Type = "tool_result"
				pj.ToolResultID = p.ID
				pj.ToolResultName = p.Name
				pj.ToolResultValue = p.Result
				pj.ToolResultError = p.Error
			default:
				// Skip unknown part types
				continue
			}
			msgJSON.Parts = append(msgJSON.Parts, pj)
		}

		data.Messages = append(data.Messages, msgJSON)
	}

	return json.Marshal(data)
}

// UnmarshalJSON deserializes a conversation from JSON.
func (c *Conversation) UnmarshalJSON(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var j conversationJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}

	c.model = j.Model
	c.system = j.System
	c.voice = j.Voice
	c.stt = j.STT
	c.maxMsgs = j.MaxMsgs
	c.metadata = j.Metadata
	c.messages = make([]core.Message, 0, len(j.Messages))

	for _, msgJSON := range j.Messages {
		msg := core.Message{
			Role:  core.Role(msgJSON.Role),
			Parts: make([]core.Part, 0, len(msgJSON.Parts)),
		}

		for _, pj := range msgJSON.Parts {
			switch pj.Type {
			case "text":
				msg.Parts = append(msg.Parts, core.Text{Text: pj.Text})
			case "audio":
				if pj.AudioBase64 != "" {
					data, err := base64.StdEncoding.DecodeString(pj.AudioBase64)
					if err != nil {
						return fmt.Errorf("invalid audio base64: %w", err)
					}
					msg.Parts = append(msg.Parts, core.Audio{
						Source: core.BlobRef{
							Kind:  core.BlobBytes,
							Bytes: data,
							MIME:  pj.AudioMIME,
							Size:  int64(len(data)),
						},
					})
				}
			case "image":
				if pj.ImageBase64 != "" {
					data, err := base64.StdEncoding.DecodeString(pj.ImageBase64)
					if err != nil {
						return fmt.Errorf("invalid image base64: %w", err)
					}
					msg.Parts = append(msg.Parts, core.Image{
						Source: core.BlobRef{
							Kind:  core.BlobBytes,
							Bytes: data,
							MIME:  pj.ImageMIME,
							Size:  int64(len(data)),
						},
					})
				}
			case "image_url":
				msg.Parts = append(msg.Parts, core.ImageURL{URL: pj.ImageURL})
			case "tool_call":
				msg.Parts = append(msg.Parts, core.ToolCall{
					ID:    pj.ToolCallID,
					Name:  pj.ToolCallName,
					Input: pj.ToolCallInput,
				})
			case "tool_result":
				msg.Parts = append(msg.Parts, core.ToolResult{
					ID:     pj.ToolResultID,
					Name:   pj.ToolResultName,
					Result: pj.ToolResultValue,
					Error:  pj.ToolResultError,
				})
			}
		}

		c.messages = append(c.messages, msg)
	}

	return nil
}

// JSON serialization types
type conversationJSON struct {
	Model    string            `json:"model"`
	System   string            `json:"system,omitempty"`
	Voice    string            `json:"voice,omitempty"`
	STT      string            `json:"stt,omitempty"`
	MaxMsgs  int               `json:"max_messages,omitempty"`
	Metadata map[string]any    `json:"metadata,omitempty"`
	Messages []messageJSON     `json:"messages"`
}

type messageJSON struct {
	Role  string     `json:"role"`
	Parts []partJSON `json:"parts"`
}

type partJSON struct {
	Type string `json:"type"`

	// Text
	Text string `json:"text,omitempty"`

	// Audio
	AudioBase64 string `json:"audio_base64,omitempty"`
	AudioMIME   string `json:"audio_mime,omitempty"`

	// Image
	ImageBase64 string `json:"image_base64,omitempty"`
	ImageMIME   string `json:"image_mime,omitempty"`
	ImageURL    string `json:"image_url,omitempty"`

	// Tool call
	ToolCallID    string         `json:"tool_call_id,omitempty"`
	ToolCallName  string         `json:"tool_call_name,omitempty"`
	ToolCallInput map[string]any `json:"tool_call_input,omitempty"`

	// Tool result
	ToolResultID    string `json:"tool_result_id,omitempty"`
	ToolResultName  string `json:"tool_result_name,omitempty"`
	ToolResultValue any    `json:"tool_result_value,omitempty"`
	ToolResultError string `json:"tool_result_error,omitempty"`
}
