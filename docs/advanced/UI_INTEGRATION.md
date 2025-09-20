# UI Integration Guide

Complete guide for integrating gai streaming events with web UIs, including React hooks, TypeScript types, and real-time rendering patterns.

## Table of Contents

1. [Overview](#overview)
2. [Streaming Architecture](#streaming-architecture)
3. [Server Setup](#server-setup)
4. [Browser Client](#browser-client)
5. [React Integration](#react-integration)
6. [TypeScript Types](#typescript-types)
7. [UI Components](#ui-components)
8. [Real-time Rendering](#real-time-rendering)
9. [Error Handling & Resilience](#error-handling--resilience)
10. [Performance Optimization](#performance-optimization)
11. [Production Patterns](#production-patterns)

## Overview

The gai SDK provides normalized streaming events (`gai.events.v1`) that can be consumed by web UIs for real-time AI interactions. This guide covers everything needed to build responsive, production-ready UIs.

### Key Features

- **Server-Sent Events (SSE)** for real-time streaming
- **NDJSON** alternative for environments without SSE
- **React hooks** for state management
- **TypeScript types** for type safety
- **Progressive rendering** of text, tools, and reasoning
- **Automatic reconnection** with exponential backoff
- **Event deduplication** and ordering guarantees

## Streaming Architecture

### Event Flow

```
┌─────────┐      ┌────────┐      ┌─────────┐      ┌────────┐
│   AI    │──────│  gai   │──────│  HTTP   │──────│  React │
│Provider │ JSON │Normalize│ SSE  │ Handler │ SSE  │   UI   │
└─────────┘      └────────┘      └─────────┘      └────────┘
     ↓               ↓                ↓                ↓
  Native         gai.events.v1    data: JSON      State Updates
  Events          Normalized       SSE Stream      React Render
```

### Protocol Comparison

| Feature | SSE | NDJSON | WebSocket |
|---------|-----|--------|-----------|
| Real-time | ✅ | ✅ | ✅ |
| Auto-reconnect | ✅ | ❌ | ❌ |
| HTTP/2 multiplexing | ✅ | ✅ | ❌ |
| Firewall friendly | ✅ | ✅ | ⚠️ |
| Browser support | ✅ | ✅ | ✅ |
| Bidirectional | ❌ | ❌ | ✅ |

## Server Setup

### Basic SSE Handler

```go
package handlers

import (
    "encoding/json"
    "fmt"
    "net/http"

    "github.com/shillcollin/gai/core"
    "github.com/shillcollin/gai/stream"
)

// StreamingHandler handles SSE requests
func StreamingHandler(provider core.Provider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Parse request body
        var req core.Request
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            http.Error(w, err.Error(), http.StatusBadRequest)
            return
        }

        // Enable streaming
        req.Stream = true

        // Create stream
        s, err := provider.StreamText(r.Context(), req)
        if err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
        defer s.Close()

        // Set SSE headers
        w.Header().Set("Content-Type", "text/event-stream")
        w.Header().Set("Cache-Control", "no-cache")
        w.Header().Set("Connection", "keep-alive")
        w.Header().Set("X-Accel-Buffering", "no") // Disable Nginx buffering

        // CORS headers for browser access
        w.Header().Set("Access-Control-Allow-Origin", "*")
        w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
        w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

        // Stream events
        if err := stream.SSE(w, s); err != nil {
            // Log error but can't send HTTP error after headers
            fmt.Printf("Streaming error: %v\n", err)
        }
    }
}
```

### Advanced SSE with UI Policy

```go
// UIStreamHandler provides UI-optimized streaming
func UIStreamHandler(provider core.Provider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        var req struct {
            core.Request
            UIOptions UIStreamOptions `json:"ui_options"`
        }

        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            http.Error(w, err.Error(), http.StatusBadRequest)
            return
        }

        s, err := provider.StreamText(r.Context(), req.Request)
        if err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
        defer s.Close()

        // Apply UI-specific policy
        policy := stream.Policy{
            SendStart:      req.UIOptions.SendStart,
            SendFinish:     req.UIOptions.SendFinish,
            SendReasoning:  req.UIOptions.SendReasoning,
            SendSources:    req.UIOptions.SendSources,
            MaskErrors:     req.UIOptions.MaskErrors,
            BufferSize:     1024,
        }

        // Set headers
        w.Header().Set("Content-Type", "text/event-stream")
        w.Header().Set("Cache-Control", "no-cache")

        // Stream with policy
        if err := stream.SSEWithPolicy(w, s, policy); err != nil {
            fmt.Printf("Streaming error: %v\n", err)
        }
    }
}

type UIStreamOptions struct {
    SendStart      bool `json:"send_start"`
    SendFinish     bool `json:"send_finish"`
    SendReasoning  bool `json:"send_reasoning"`
    SendSources    bool `json:"send_sources"`
    MaskErrors     bool `json:"mask_errors"`
}
```

### NDJSON Alternative

```go
// NDJSONHandler for environments without SSE support
func NDJSONHandler(provider core.Provider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        var req core.Request
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            http.Error(w, err.Error(), http.StatusBadRequest)
            return
        }

        req.Stream = true
        s, err := provider.StreamText(r.Context(), req)
        if err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
        defer s.Close()

        // Set NDJSON headers
        w.Header().Set("Content-Type", "application/x-ndjson")
        w.Header().Set("X-Content-Type-Options", "nosniff")

        // Stream as NDJSON
        if err := stream.NDJSON(w, s); err != nil {
            fmt.Printf("Streaming error: %v\n", err)
        }
    }
}
```

### Complete HTTP Server

```go
package main

import (
    "log"
    "net/http"
    "os"

    "github.com/shillcollin/gai/providers/openai"
)

func main() {
    // Initialize provider
    provider := openai.New(
        openai.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
        openai.WithModel("gpt-4o-mini"),
    )

    // Setup routes
    mux := http.NewServeMux()

    // Streaming endpoints
    mux.HandleFunc("/stream", StreamingHandler(provider))
    mux.HandleFunc("/stream/ui", UIStreamHandler(provider))
    mux.HandleFunc("/stream/ndjson", NDJSONHandler(provider))

    // CORS preflight
    mux.HandleFunc("/stream/", func(w http.ResponseWriter, r *http.Request) {
        if r.Method == "OPTIONS" {
            w.Header().Set("Access-Control-Allow-Origin", "*")
            w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
            w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
            w.WriteHeader(http.StatusOK)
            return
        }
    })

    // Start server
    log.Printf("Server starting on :8080")
    if err := http.ListenAndServe(":8080", mux); err != nil {
        log.Fatal(err)
    }
}
```

## Browser Client

### Vanilla JavaScript SSE Client

```javascript
class GaiStreamClient {
    constructor(endpoint = '/stream') {
        this.endpoint = endpoint;
        this.eventSource = null;
        this.reconnectAttempts = 0;
        this.maxReconnectAttempts = 5;
        this.reconnectDelay = 1000;
        this.events = [];
        this.handlers = new Map();
        this.streamId = null;
        this.lastSeq = -1;
    }

    async stream(request, options = {}) {
        // Close existing connection
        this.close();

        // Reset state
        this.events = [];
        this.streamId = null;
        this.lastSeq = -1;

        // Make POST request to get stream
        const response = await fetch(this.endpoint, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                ...options.headers
            },
            body: JSON.stringify(request)
        });

        if (!response.ok) {
            throw new Error(`HTTP ${response.status}: ${response.statusText}`);
        }

        // Parse SSE stream
        return this.parseSSE(response.body.getReader());
    }

    async parseSSE(reader) {
        const decoder = new TextDecoder();
        let buffer = '';

        while (true) {
            const { done, value } = await reader.read();

            if (done) break;

            buffer += decoder.decode(value, { stream: true });
            const lines = buffer.split('\n');

            // Keep incomplete line in buffer
            buffer = lines.pop() || '';

            for (const line of lines) {
                if (line.startsWith('data: ')) {
                    const data = line.slice(6);
                    if (data === '[DONE]') {
                        this.handleComplete();
                        return;
                    }

                    try {
                        const event = JSON.parse(data);
                        this.handleEvent(event);
                    } catch (e) {
                        console.error('Failed to parse event:', e);
                    }
                }
            }
        }
    }

    handleEvent(event) {
        // Validate schema
        if (event.schema !== 'gai.events.v1') {
            console.warn('Unknown schema:', event.schema);
            return;
        }

        // Check sequence
        if (this.streamId && event.stream_id !== this.streamId) {
            console.warn('Stream ID mismatch');
            return;
        }

        if (event.seq <= this.lastSeq) {
            console.warn('Out of order event:', event.seq);
            return;
        }

        this.streamId = event.stream_id;
        this.lastSeq = event.seq;
        this.events.push(event);

        // Dispatch to handlers
        this.emit(event.type, event);
        this.emit('event', event);

        // Handle terminal events
        if (event.type === 'finish' || event.type === 'error') {
            this.handleComplete();
        }
    }

    on(eventType, handler) {
        if (!this.handlers.has(eventType)) {
            this.handlers.set(eventType, new Set());
        }
        this.handlers.get(eventType).add(handler);
        return () => this.off(eventType, handler);
    }

    off(eventType, handler) {
        this.handlers.get(eventType)?.delete(handler);
    }

    emit(eventType, data) {
        this.handlers.get(eventType)?.forEach(handler => {
            try {
                handler(data);
            } catch (e) {
                console.error('Handler error:', e);
            }
        });
    }

    handleComplete() {
        this.emit('complete', this.events);
    }

    close() {
        if (this.eventSource) {
            this.eventSource.close();
            this.eventSource = null;
        }
    }
}

// Usage
const client = new GaiStreamClient('/stream');

client.on('text.delta', (event) => {
    document.getElementById('output').textContent += event.text;
});

client.on('tool.call', (event) => {
    console.log('Tool called:', event.tool_call);
});

client.on('finish', (event) => {
    console.log('Stream finished:', event.usage);
});

await client.stream({
    messages: [
        { role: 'user', parts: [{ text: 'Hello!' }] }
    ]
});
```

### Fetch-based Streaming

```javascript
async function streamChat(request) {
    const response = await fetch('/stream', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(request)
    });

    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';

    while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop() || '';

        for (const line of lines) {
            if (line.startsWith('data: ')) {
                const data = line.slice(6).trim();
                if (data && data !== '[DONE]') {
                    const event = JSON.parse(data);
                    processEvent(event);
                }
            }
        }
    }
}

function processEvent(event) {
    switch (event.type) {
        case 'text.delta':
            appendText(event.text);
            break;
        case 'tool.call':
            showToolCall(event.tool_call);
            break;
        case 'tool.result':
            showToolResult(event.tool_result);
            break;
        case 'finish':
            showUsage(event.usage);
            break;
    }
}
```

## React Integration

### React Hook for Streaming

```tsx
import { useState, useCallback, useRef, useEffect } from 'react';

// Note: The `GaiEvent` type is a discriminated union of all possible event types.
// See the "Complete Type Definitions" section for the full definition.

export interface StreamState {
    messages: Message[];
    currentText: string;
    toolCalls: ToolCall[];
    citations: Citation[];
    isStreaming: boolean;
    error: Error | null;
    usage: Usage | null;
}

export function useGaiStream(endpoint = '/stream') {
    const [state, setState] = useState<StreamState>({
        messages: [],
        currentText: '',
        toolCalls: [],
        citations: [],
        isStreaming: false,
        error: null,
        usage: null
    });

    const abortControllerRef = useRef<AbortController | null>(null);
    const eventBufferRef = useRef<GaiEvent[]>([]);

    const processEvent = useCallback((event: GaiEvent) => {
        setState(prev => {
            switch (event.type) {
                case 'start':
                    return {
                        ...prev,
                        currentText: '',
                        toolCalls: [],
                        citations: [],
                        error: null
                    };

                case 'text.delta':
                    return {
                        ...prev,
                        currentText: prev.currentText + (event.text || '')
                    };

                case 'tool.call':
                    return {
                        ...prev,
                        toolCalls: [...prev.toolCalls, event.tool_call!]
                    };

                case 'tool.result':
                    return {
                        ...prev,
                        toolCalls: prev.toolCalls.map(tc =>
                            tc.call_id === event.tool_result?.call_id
                                ? { ...tc, result: event.tool_result }
                                : tc
                        )
                    };

                case 'citations':
                    return {
                        ...prev,
                        citations: [...prev.citations, ...(event.citations || [])]
                    };

                case 'finish':
                    const finalMessage: Message = {
                        role: 'assistant',
                        parts: [{ type: 'text', text: prev.currentText }]
                    };

                    // Add tool results as parts
                    if (prev.toolCalls.length > 0) {
                        finalMessage.parts.push(
                            ...prev.toolCalls.map(tc => ({
                                type: 'tool_call' as const,
                                ...tc
                            }))
                        );
                    }

                    return {
                        ...prev,
                        messages: [...prev.messages, finalMessage],
                        currentText: '',
                        isStreaming: false,
                        usage: event.usage || null
                    };

                case 'error':
                    return {
                        ...prev,
                        error: new Error(event.error?.message || 'Stream error'),
                        isStreaming: false
                    };

                default:
                    return prev;
            }
        });
    }, []);

    const stream = useCallback(async (request: Request) => {
        // Cancel any existing stream
        if (abortControllerRef.current) {
            abortControllerRef.current.abort();
        }

        // Create new controller
        const controller = new AbortController();
        abortControllerRef.current = controller;

        // Reset state
        setState(prev => ({
            ...prev,
            isStreaming: true,
            error: null,
            currentText: '',
            toolCalls: [],
            citations: []
        }));

        // Clear event buffer
        eventBufferRef.current = [];

        try {
            const response = await fetch(endpoint, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(request),
                signal: controller.signal
            });

            if (!response.ok) {
                throw new Error(`HTTP ${response.status}`);
            }

            const reader = response.body!.getReader();
            const decoder = new TextDecoder();
            let buffer = '';

            while (true) {
                const { done, value } = await reader.read();
                if (done) break;

                buffer += decoder.decode(value, { stream: true });
                const lines = buffer.split('\n');
                buffer = lines.pop() || '';

                for (const line of lines) {
                    if (line.startsWith('data: ')) {
                        const data = line.slice(6).trim();
                        if (data && data !== '[DONE]') {
                            try {
                                const event = JSON.parse(data);
                                eventBufferRef.current.push(event);
                                processEvent(event);
                            } catch (e) {
                                console.error('Parse error:', e);
                            }
                        }
                    }
                }
            }
        } catch (error: any) {
            if (error.name !== 'AbortError') {
                setState(prev => ({
                    ...prev,
                    error,
                    isStreaming: false
                }));
            }
        }
    }, [endpoint, processEvent]);

    const cancel = useCallback(() => {
        if (abortControllerRef.current) {
            abortControllerRef.current.abort();
            abortControllerRef.current = null;
        }
        setState(prev => ({ ...prev, isStreaming: false }));
    }, []);

    const sendMessage = useCallback(async (text: string) => {
        const userMessage: Message = {
            role: 'user',
            parts: [{ type: 'text', text }]
        };

        setState(prev => ({
            ...prev,
            messages: [...prev.messages, userMessage]
        }));

        await stream({
            messages: [...state.messages, userMessage]
        });
    }, [state.messages, stream]);

    const reset = useCallback(() => {
        cancel();
        setState({
            messages: [],
            currentText: '',
            toolCalls: [],
            citations: [],
            isStreaming: false,
            error: null,
            usage: null
        });
    }, [cancel]);

    // Cleanup on unmount
    useEffect(() => {
        return () => {
            if (abortControllerRef.current) {
                abortControllerRef.current.abort();
            }
        };
    }, []);

    return {
        ...state,
        sendMessage,
        cancel,
        reset,
        events: eventBufferRef.current
    };
}
```

### React Component Example

```tsx
import React, { useState } from 'react';
import { useGaiStream } from './hooks/useGaiStream';

export function ChatInterface() {
    const [input, setInput] = useState('');
    const {
        messages,
        currentText,
        toolCalls,
        citations,
        isStreaming,
        error,
        usage,
        sendMessage,
        cancel,
        reset
    } = useGaiStream('/stream');

    const handleSubmit = (e: React.FormEvent) => {
        e.preventDefault();
        if (input.trim() && !isStreaming) {
            sendMessage(input);
            setInput('');
        }
    };

    return (
        <div className="chat-interface">
            <div className="messages">
                {messages.map((msg, idx) => (
                    <Message key={idx} message={msg} />
                ))}

                {/* Current streaming message */}
                {isStreaming && currentText && (
                    <div className="message assistant streaming">
                        <MessageContent text={currentText} />
                        <StreamingIndicator />
                    </div>
                )}

                {/* Active tool calls */}
                {toolCalls.length > 0 && (
                    <ToolCallsDisplay toolCalls={toolCalls} />
                )}

                {/* Citations */}
                {citations.length > 0 && (
                    <CitationsDisplay citations={citations} />
                )}

                {/* Error display */}
                {error && (
                    <div className="error">
                        Error: {error.message}
                    </div>
                )}
            </div>

            <form onSubmit={handleSubmit} className="input-form">
                <input
                    type="text"
                    value={input}
                    onChange={(e) => setInput(e.target.value)}
                    disabled={isStreaming}
                    placeholder="Type a message..."
                />

                {isStreaming ? (
                    <button type="button" onClick={cancel}>
                        Cancel
                    </button>
                ) : (
                    <button type="submit" disabled={!input.trim()}>
                        Send
                    </button>
                )}
            </form>

            {usage && (
                <div className="usage">
                    Tokens: {usage.total_tokens} | Cost: ${usage.cost_usd?.toFixed(4)}
                </div>
            )}
        </div>
    );
}

function Message({ message }: { message: Message }) {
    return (
        <div className={`message ${message.role}`}>
            <div className="role">{message.role}</div>
            {message.parts.map((part, idx) => (
                <MessagePart key={idx} part={part} />
            ))}
        </div>
    );
}

function MessagePart({ part }: { part: MessagePart }) {
    switch (part.type) {
        case 'text':
            return <MessageContent text={part.text} />;
        case 'tool_call':
            return <ToolCallDisplay call={part} />;
        case 'image':
            return <img src={part.url} alt={part.alt} />;
        default:
            return null;
    }
}

function MessageContent({ text }: { text: string }) {
    // Render markdown or plain text
    return <div className="content">{text}</div>;
}

function StreamingIndicator() {
    return (
        <span className="streaming-indicator">
            <span className="dot"></span>
            <span className="dot"></span>
            <span className="dot"></span>
        </span>
    );
}
```

## TypeScript Types

### Complete Type Definitions

```typescript
// gai.events.v1.d.ts

export interface GaiEvent {
    type: EventType;
    schema: 'gai.events.v1';
    seq: number;
    ts: string;
    stream_id: string;
    request_id?: string;
    provider?: string;
    model?: string;
    trace_id?: string;
    span_id?: string;
    prompt_id?: PromptID;
    ext?: Record<string, any>;
}

export enum EventType {
    Start = 'start',
    TextDelta = 'text.delta',
    ReasoningDelta = 'reasoning.delta',
    ReasoningSummary = 'reasoning.summary',
    AudioDelta = 'audio.delta',
    ToolCall = 'tool.call',
    ToolResult = 'tool.result',
    Citations = 'citations',
    Safety = 'safety',
    StepStart = 'step.start',
    StepFinish = 'step.finish',
    Finish = 'finish',
    Error = 'error'
}

export interface StartEvent extends GaiEvent {
    type: EventType.Start;
    capabilities?: string[];
    policies?: {
        reasoning_visibility?: 'none' | 'summary' | 'raw';
        [key: string]: any;
    };
}

export interface TextDeltaEvent extends GaiEvent {
    type: EventType.TextDelta;
    text: string;
    partial?: boolean;
    cursor?: {
        chars_emitted: number;
    };
    step_id?: number;
    role?: 'assistant';
}

export interface ReasoningDeltaEvent extends GaiEvent {
    type: EventType.ReasoningDelta;
    text: string;
    partial?: boolean;
    cursor?: {
        tokens_emitted: number;
    };
    step_id?: number;
    visibility?: 'raw';
    notice?: string;
}

export interface ReasoningSummaryEvent extends GaiEvent {
    type: EventType.ReasoningSummary;
    summary: string;
    steps?: string[];
    confidence?: number;
    tokens?: number;
    method?: 'model' | 'heuristic' | 'provider';
    step_id?: number;
    visibility?: 'summary';
    signatures?: Array<{
        provider: string;
        type: string;
        value: string;
    }>;
}

export interface AudioDeltaEvent extends GaiEvent {
    type: EventType.AudioDelta;
    audio_b64: string;
    format: string;
    partial?: boolean;
    step_id?: number;
}

export interface ToolCallEvent extends GaiEvent {
    type: EventType.ToolCall;
    tool_call: ToolCall;
    step_id?: number;
}

export interface ToolResultEvent extends GaiEvent {
    type: EventType.ToolResult;
    tool_result: ToolResult;
    step_id?: number;
}

export interface CitationsEvent extends GaiEvent {
    type: EventType.Citations;
    citations: Citation[];
    step_id?: number;
}

export interface SafetyEvent extends GaiEvent {
    type: EventType.Safety;
    safety: Safety;
    step_id?: number;
}

export interface StepStartEvent extends GaiEvent {
    type: EventType.StepStart;
    step_id: number;
}

export interface StepFinishEvent extends GaiEvent {
    type: EventType.StepFinish;
    step_id: number;
    duration_ms?: number;
    tool_calls?: number;
    text_len?: number;
}

export interface FinishEvent extends GaiEvent {
    type: EventType.Finish;
    finish_reason: StopReason;
    usage?: Usage;
    final?: true;
}

export interface ErrorEvent extends GaiEvent {
    type: EventType.Error;
    error: ErrorObject;
    step_id?: number;
}

// Supporting types

export interface ToolCall {
    call_id: string;
    name: string;
    input: Record<string, any>;
    timeout_ms?: number;
    parallel_group?: number;
}

export interface ToolResult {
    call_id: string;
    ok?: boolean;
    result?: any;
    error?: ErrorObject;
    duration_ms?: number;
}

export interface Citation {
    uri: string;
    title?: string;
    snippet?: string;
    start?: number;
    end?: number;
    score?: number;
}

export interface Safety {
    category: 'harassment' | 'hate' | 'sexual' | 'dangerous' | 'selfharm' | 'other';
    action: 'allow' | 'block' | 'redact' | 'safe_completion';
    score?: number;
    target?: 'input' | 'output';
    start?: number;
    end?: number;
}

export interface StopReason {
    type: 'stop' | 'length' | 'content_filter' | 'tool_calls_exhausted' | 'max_steps' | 'aborted' | 'error';
    description?: string;
    details?: Record<string, any>;
}

export interface Usage {
    input_tokens?: number;
    output_tokens?: number;
    reasoning_tokens?: number;
    total_tokens?: number;
    cost_usd?: number;
}

export interface ErrorObject {
    code: ErrorCode;
    message: string;
    status?: number;
    retryable?: boolean;
    details?: Record<string, any>;
}

export enum ErrorCode {
    RateLimited = 'rate_limited',
    ContentFiltered = 'content_filtered',
    BadRequest = 'bad_request',
    Transient = 'transient',
    ProviderError = 'provider_error',
    Timeout = 'timeout',
    Canceled = 'canceled',
    ToolError = 'tool_error',
    Internal = 'internal'
}

export interface PromptID {
    name: string;
    version?: string;
    fingerprint?: string;
}

// Request/Response types

export interface Message {
    role: 'system' | 'user' | 'assistant';
    parts: MessagePart[];
    name?: string;
    metadata?: Record<string, any>;
}

export interface MessagePart {
    type: string;
    [key: string]: any;
}

export interface TextPart extends MessagePart {
    type: 'text';
    text: string;
}

export interface Request {
    messages: Message[];
    model?: string;
    temperature?: number;
    max_tokens?: number;
    top_p?: number;
    top_k?: number;
    stream?: boolean;
    tools?: Tool[];
    tool_choice?: 'auto' | 'none' | 'required';
    safety?: SafetyConfig;
    session?: Session;
    provider_options?: Record<string, any>;
    metadata?: Record<string, any>;
}

export interface SafetyConfig {
    harassment?: SafetyLevel;
    hate?: SafetyLevel;
    sexual?: SafetyLevel;
    dangerous?: SafetyLevel;
    self_harm?: SafetyLevel;
    other?: SafetyLevel;
}

export type SafetyLevel = 'none' | 'low' | 'medium' | 'high' | 'block';

export interface Session {
    provider: string;
    id: string;
    created_at?: string;
    expires_at?: string;
    metadata?: Record<string, any>;
}

export interface Tool {
    name: string;
    description: string;
    input_schema: JSONSchema;
}

export interface JSONSchema {
    type: string;
    properties?: Record<string, JSONSchema>;
    required?: string[];
    items?: JSONSchema;
    [key: string]: any;
}
```

## UI Components

### Markdown Renderer with Code Highlighting

```tsx
import React, { memo } from 'react';
import ReactMarkdown from 'react-markdown';
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter';
import { vscDarkPlus } from 'react-syntax-highlighter/dist/esm/styles/prism';

export const MarkdownMessage = memo(({ content }: { content: string }) => {
    return (
        <ReactMarkdown
            components={{
                code({ node, inline, className, children, ...props }) {
                    const match = /language-(\w+)/.exec(className || '');
                    return !inline && match ? (
                        <SyntaxHighlighter
                            style={vscDarkPlus}
                            language={match[1]}
                            PreTag="div"
                            {...props}
                        >
                            {String(children).replace(/\n$/, '')}
                        </SyntaxHighlighter>
                    ) : (
                        <code className={className} {...props}>
                            {children}
                        </code>
                    );
                }
            }}
        >
            {content}
        </ReactMarkdown>
    );
});
```

### Tool Call Visualization

```tsx
import React from 'react';
import { ToolCall, ToolResult } from './types';

export function ToolCallDisplay({
    call,
    result
}: {
    call: ToolCall;
    result?: ToolResult;
}) {
    const [expanded, setExpanded] = useState(false);

    return (
        <div className="tool-call">
            <div
                className="tool-header"
                onClick={() => setExpanded(!expanded)}
            >
                <span className="tool-icon">🔧</span>
                <span className="tool-name">{call.name}</span>
                <span className={`tool-status ${result?.ok ? 'success' : result ? 'error' : 'pending'}`}>
                    {result ? (result.ok ? '✓' : '✗') : '⋯'}
                </span>
            </div>

            {expanded && (
                <div className="tool-details">
                    <div className="tool-input">
                        <strong>Input:</strong>
                        <pre>{JSON.stringify(call.input, null, 2)}</pre>
                    </div>

                    {result && (
                        <div className="tool-output">
                            <strong>Output:</strong>
                            {result.ok ? (
                                <pre>{JSON.stringify(result.result, null, 2)}</pre>
                            ) : (
                                <div className="error">
                                    {result.error?.message}
                                </div>
                            )}
                            {result.duration_ms && (
                                <div className="timing">
                                    Duration: {result.duration_ms}ms
                                </div>
                            )}
                        </div>
                    )}
                </div>
            )}
        </div>
    );
}
```

### Reasoning Display

```tsx
export function ReasoningDisplay({
    summary,
    steps,
    visibility
}: {
    summary?: string;
    steps?: string[];
    visibility?: string;
}) {
    const [showReasoning, setShowReasoning] = useState(false);

    if (!summary && !steps?.length) return null;

    return (
        <div className="reasoning-container">
            <button
                className="reasoning-toggle"
                onClick={() => setShowReasoning(!showReasoning)}
            >
                💭 {showReasoning ? 'Hide' : 'Show'} Reasoning
            </button>

            {showReasoning && (
                <div className="reasoning-content">
                    {summary && (
                        <div className="reasoning-summary">
                            {summary}
                        </div>
                    )}

                    {steps && steps.length > 0 && (
                        <ol className="reasoning-steps">
                            {steps.map((step, idx) => (
                                <li key={idx}>{step}</li>
                            ))}
                        </ol>
                    )}

                    {visibility === 'raw' && (
                        <div className="reasoning-notice">
                            ⚠️ Raw reasoning shown (development mode)
                        </div>
                    )}
                </div>
            )}
        </div>
    );
}
```

## Real-time Rendering

### Progressive Text Rendering

```tsx
export function StreamingText({ text }: { text: string }) {
    const [displayText, setDisplayText] = useState('');
    const [cursor, setCursor] = useState(0);

    useEffect(() => {
        // Animate text appearance
        const timer = setInterval(() => {
            if (cursor < text.length) {
                const chunk = text.slice(cursor, cursor + 5);
                setDisplayText(prev => prev + chunk);
                setCursor(prev => prev + chunk.length);
            } else {
                clearInterval(timer);
            }
        }, 10);

        return () => clearInterval(timer);
    }, [text, cursor]);

    return (
        <span>
            {displayText}
            {cursor < text.length && <span className="cursor">▊</span>}
        </span>
    );
}
```

### Citation Rendering

```tsx
export function CitationRenderer({
    text,
    citations
}: {
    text: string;
    citations: Citation[];
}) {
    // Sort citations by start position
    const sortedCitations = [...citations].sort((a, b) =>
        (a.start || 0) - (b.start || 0)
    );

    // Build text with citation markers
    const parts: React.ReactNode[] = [];
    let lastEnd = 0;

    sortedCitations.forEach((citation, idx) => {
        const start = citation.start || 0;
        const end = citation.end || start;

        // Add text before citation
        if (start > lastEnd) {
            parts.push(text.slice(lastEnd, start));
        }

        // Add cited text with marker
        parts.push(
            <span key={idx} className="cited-text">
                {text.slice(start, end)}
                <sup className="citation-marker">
                    <a
                        href={citation.uri}
                        title={citation.title}
                        target="_blank"
                        rel="noopener noreferrer"
                    >
                        [{idx + 1}]
                    </a>
                </sup>
            </span>
        );

        lastEnd = end;
    });

    // Add remaining text
    if (lastEnd < text.length) {
        parts.push(text.slice(lastEnd));
    }

    return (
        <>
            <div className="cited-content">{parts}</div>
            {citations.length > 0 && (
                <div className="citations-list">
                    <h4>Sources</h4>
                    {sortedCitations.map((citation, idx) => (
                        <div key={idx} className="citation">
                            <span className="citation-number">[{idx + 1}]</span>
                            <a
                                href={citation.uri}
                                target="_blank"
                                rel="noopener noreferrer"
                            >
                                {citation.title || citation.uri}
                            </a>
                            {citation.snippet && (
                                <p className="citation-snippet">{citation.snippet}</p>
                            )}
                        </div>
                    ))}
                </div>
            )}
        </>
    );
}
```

## Error Handling & Resilience

### Automatic Reconnection

```typescript
class ResilientStreamClient {
    private reconnectAttempts = 0;
    private maxReconnectAttempts = 5;
    private reconnectDelay = 1000;
    private backoffMultiplier = 1.5;

    async streamWithReconnect(
        request: Request,
        onEvent: (event: GaiEvent) => void
    ): Promise<void> {
        while (this.reconnectAttempts < this.maxReconnectAttempts) {
            try {
                await this.stream(request, onEvent);
                // Successful completion
                this.reconnectAttempts = 0;
                return;
            } catch (error) {
                if (!this.shouldReconnect(error)) {
                    throw error;
                }

                const delay = this.calculateBackoff();
                console.log(`Reconnecting in ${delay}ms...`);
                await this.sleep(delay);
                this.reconnectAttempts++;
            }
        }

        throw new Error('Max reconnection attempts reached');
    }

    private shouldReconnect(error: any): boolean {
        // Don't reconnect on user cancellation
        if (error.name === 'AbortError') return false;

        // Don't reconnect on client errors
        if (error.status >= 400 && error.status < 500) return false;

        // Reconnect on network errors and server errors
        return true;
    }

    private calculateBackoff(): number {
        const jitter = Math.random() * 200;
        return Math.min(
            this.reconnectDelay * Math.pow(this.backoffMultiplier, this.reconnectAttempts) + jitter,
            30000 // Max 30 seconds
        );
    }

    private sleep(ms: number): Promise<void> {
        return new Promise(resolve => setTimeout(resolve, ms));
    }
}
```

### Event Deduplication

```typescript
class EventDeduplicator {
    private seenEvents = new Map<string, Set<number>>();

    isDuplicate(event: GaiEvent): boolean {
        const streamEvents = this.seenEvents.get(event.stream_id) || new Set();

        if (streamEvents.has(event.seq)) {
            return true;
        }

        streamEvents.add(event.seq);
        this.seenEvents.set(event.stream_id, streamEvents);

        // Clean up old streams
        if (this.seenEvents.size > 10) {
            const oldestKey = this.seenEvents.keys().next().value;
            this.seenEvents.delete(oldestKey);
        }

        return false;
    }

    reset(): void {
        this.seenEvents.clear();
    }
}
```

## Performance Optimization

### Virtual Scrolling for Long Conversations

```tsx
import { FixedSizeList as List } from 'react-window';

export function VirtualizedChat({ messages }: { messages: Message[] }) {
    const Row = ({ index, style }: { index: number; style: React.CSSProperties }) => (
        <div style={style}>
            <Message message={messages[index]} />
        </div>
    );

    return (
        <List
            height={600}
            itemCount={messages.length}
            itemSize={120} // Estimated height
            width="100%"
        >
            {Row}
        </List>
    );
}
```

### Debounced Updates

```typescript
function useDebounced<T>(value: T, delay: number): T {
    const [debouncedValue, setDebouncedValue] = useState(value);

    useEffect(() => {
        const timer = setTimeout(() => {
            setDebouncedValue(value);
        }, delay);

        return () => clearTimeout(timer);
    }, [value, delay]);

    return debouncedValue;
}

// Usage in streaming
function StreamingDisplay() {
    const { currentText } = useGaiStream();
    const debouncedText = useDebounced(currentText, 50);

    return <div>{debouncedText}</div>;
}
```

## Production Patterns

### Complete Chat Application

```tsx
import React, { useState, useRef, useEffect } from 'react';
import { useGaiStream } from './hooks/useGaiStream';
import { ChatMessage } from './components/ChatMessage';
import { ChatInput } from './components/ChatInput';
import { ChatSettings } from './components/ChatSettings';

export function ChatApplication() {
    const messagesEndRef = useRef<HTMLDivElement>(null);
    const [settings, setSettings] = useState({
        model: 'gpt-4o-mini',
        temperature: 0.7,
        maxTokens: 1000,
        streamReasoning: false,
        showSources: true
    });

    const {
        messages,
        currentText,
        toolCalls,
        citations,
        isStreaming,
        error,
        usage,
        sendMessage,
        cancel,
        reset
    } = useGaiStream('/stream', settings);

    // Auto-scroll to bottom
    useEffect(() => {
        messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
    }, [messages, currentText]);

    // Calculate total cost
    const totalCost = messages.reduce((sum, msg) =>
        sum + (msg.usage?.cost_usd || 0), 0
    );

    return (
        <div className="chat-app">
            <header className="chat-header">
                <h1>AI Chat</h1>
                <ChatSettings
                    settings={settings}
                    onChange={setSettings}
                />
            </header>

            <main className="chat-main">
                <div className="messages-container">
                    {messages.map((msg, idx) => (
                        <ChatMessage
                            key={idx}
                            message={msg}
                            showSources={settings.showSources}
                        />
                    ))}

                    {currentText && (
                        <div className="streaming-message">
                            <MarkdownMessage content={currentText} />
                            {toolCalls.length > 0 && (
                                <ToolCallsInProgress calls={toolCalls} />
                            )}
                        </div>
                    )}

                    {error && (
                        <ErrorDisplay error={error} onRetry={() => {
                            // Retry last message
                            const lastUserMsg = messages.findLast(m => m.role === 'user');
                            if (lastUserMsg) {
                                sendMessage(lastUserMsg.parts[0].text);
                            }
                        }} />
                    )}

                    <div ref={messagesEndRef} />
                </div>

                {citations.length > 0 && settings.showSources && (
                    <CitationsPanel citations={citations} />
                )}
            </main>

            <footer className="chat-footer">
                <ChatInput
                    onSend={sendMessage}
                    disabled={isStreaming}
                    onCancel={cancel}
                />

                <div className="chat-stats">
                    <span>Messages: {messages.length}</span>
                    <span>Tokens: {usage?.total_tokens || 0}</span>
                    <span>Cost: ${totalCost.toFixed(4)}</span>
                </div>
            </footer>
        </div>
    );
}
```

### CSS Styles

```css
/* Streaming animation */
@keyframes pulse {
    0%, 100% { opacity: 0.3; }
    50% { opacity: 1; }
}

.streaming-indicator .dot {
    animation: pulse 1.4s ease-in-out infinite;
}

.streaming-indicator .dot:nth-child(2) {
    animation-delay: 0.2s;
}

.streaming-indicator .dot:nth-child(3) {
    animation-delay: 0.4s;
}

/* Cursor animation */
@keyframes blink {
    0%, 50% { opacity: 1; }
    51%, 100% { opacity: 0; }
}

.cursor {
    animation: blink 1s step-end infinite;
}

/* Tool call status */
.tool-status.success { color: #10b981; }
.tool-status.error { color: #ef4444; }
.tool-status.pending { color: #f59e0b; }

/* Citation markers */
.cited-text {
    background: rgba(59, 130, 246, 0.1);
    border-bottom: 1px dashed #3b82f6;
}

.citation-marker {
    color: #3b82f6;
    font-weight: bold;
}
```

## Conclusion

This guide provides everything needed to integrate gai streaming with web UIs:

- **Server setup** with SSE and NDJSON endpoints
- **Browser clients** with reconnection and error handling
- **React integration** with hooks and components
- **TypeScript types** for type safety
- **UI components** for rich interactions
- **Performance optimization** techniques
- **Production patterns** for real applications

The normalized `gai.events.v1` stream format ensures consistent UI behavior across all AI providers, while the React hooks and components make it easy to build responsive, real-time AI interfaces.