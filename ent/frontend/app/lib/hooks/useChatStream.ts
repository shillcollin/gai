"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type {
  ChatMessage,
  ChatRequestPayload,
  ProviderOption,
  StreamEvent,
  ToolCallState,
  UsageSummary
} from "../types";

const API_BASE = process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:8080";

interface UseChatOptions {
  provider: string;
  model: string;
  temperature: number;
  topP: number;
  maxTokens: number;
  reasoningMode: "none" | "summary" | "raw";
}

interface ChatState {
  messages: ChatMessage[];
  pendingAssistant?: ChatMessage;
  toolCalls: ToolCallState[];
  reasoning: string[];
  reasoningSummary?: string;
  usage?: UsageSummary;
  error?: string;
}

interface UseChatStreamResult {
  state: ChatState;
  isStreaming: boolean;
  sendMessage: (text: string) => Promise<void>;
  cancel: () => void;
  setOptions: (options: Partial<UseChatOptions>) => void;
  options: UseChatOptions;
}

const INITIAL_OPTIONS: UseChatOptions = {
  provider: "openai",
  model: "gpt-4o-mini",
  temperature: 0.7,
  topP: 0.9,
  maxTokens: 400,
  reasoningMode: "summary"
};

export function useChatStream(): UseChatStreamResult {
  const [options, setOptionsState] = useState(INITIAL_OPTIONS);
  const [state, setState] = useState<ChatState>({
    messages: [],
    toolCalls: [],
    reasoning: []
  });
  const [isStreaming, setIsStreaming] = useState(false);
  const abortRef = useRef<AbortController | null>(null);

  const setOptions = useCallback((partial: Partial<UseChatOptions>) => {
    setOptionsState(prev => ({ ...prev, ...partial }));
  }, []);

  const resetStreamingState = useCallback(() => {
    setState(prev => ({
      messages: prev.messages,
      toolCalls: [],
      reasoning: [],
      usage: undefined,
      error: undefined,
      pendingAssistant: undefined,
      reasoningSummary: undefined
    }));
  }, []);

  const cancel = useCallback(() => {
    abortRef.current?.abort();
    abortRef.current = null;
    setIsStreaming(false);
  }, []);

  const pushUserMessage = useCallback((text: string) => {
    const message: ChatMessage = {
      id: crypto.randomUUID(),
      role: "user",
      parts: [{ type: "text", text }]
    };
    setState(prev => ({
      ...prev,
      messages: [...prev.messages, message]
    }));
    return message;
  }, []);

  const appendAssistantDelta = useCallback((delta: string) => {
    setState(prev => {
      const pending = prev.pendingAssistant ?? {
        id: crypto.randomUUID(),
        role: "assistant",
        parts: [{ type: "text", text: "" }],
        toolCalls: [],
        reasoning: []
      };
      const nextPart = { ...pending.parts[0], text: (pending.parts[0]?.text ?? "") + delta };
      const updatedPending: ChatMessage = {
        ...pending,
        parts: [nextPart]
      };
      return {
        ...prev,
        pendingAssistant: updatedPending
      };
    });
  }, []);

  const appendReasoningDelta = useCallback((delta: string) => {
    setState(prev => {
      const pending = prev.pendingAssistant ?? {
        id: crypto.randomUUID(),
        role: "assistant",
        parts: [{ type: "text", text: "" }],
        toolCalls: [],
        reasoning: []
      };
      const reasoning = [...(pending.reasoning ?? []), delta];
      const updatedPending: ChatMessage = {
        ...pending,
        reasoning
      };
      return {
        ...prev,
        pendingAssistant: updatedPending
      };
    });
  }, []);

  const setReasoningSummary = useCallback((summary: string) => {
    setState(prev => {
      const pending = prev.pendingAssistant ?? {
        id: crypto.randomUUID(),
        role: "assistant",
        parts: [{ type: "text", text: "" }],
        toolCalls: [],
        reasoning: []
      };
      const updatedPending: ChatMessage = {
        ...pending,
        reasoningSummary: summary
      };
      return {
        ...prev,
        pendingAssistant: updatedPending
      };
    });
  }, []);

  const updateToolCall = useCallback((call: ToolCallState) => {
    setState(prev => {
      const pending = prev.pendingAssistant ?? {
        id: crypto.randomUUID(),
        role: "assistant",
        parts: [{ type: "text", text: "" }],
        toolCalls: [],
        reasoning: []
      };
      const existing = pending.toolCalls ?? [];
      const idx = existing.findIndex(tc => tc.id === call.id);
      const nextToolCalls = [...existing];
      if (idx >= 0) {
        nextToolCalls[idx] = { ...existing[idx], ...call };
      } else {
        nextToolCalls.push(call);
      }
      const updatedPending: ChatMessage = { ...pending, toolCalls: nextToolCalls };
      return {
        ...prev,
        pendingAssistant: updatedPending
      };
    });
  }, []);

  const finalizeAssistantMessage = useCallback((usage?: UsageSummary) => {
    setState(prev => {
      if (!prev.pendingAssistant) {
        return prev;
      }
      const messages = [...prev.messages, prev.pendingAssistant];
      return {
        messages,
        pendingAssistant: undefined,
        toolCalls: [],
        reasoning: [],
        reasoningSummary: undefined,
        usage: usage ?? prev.usage,
        error: undefined
      };
    });
  }, []);

  const emitError = useCallback((errMsg: string) => {
    setState(prev => ({
      ...prev,
      error: errMsg
    }));
  }, []);

  const buildPayload = useCallback((text: string): ChatRequestPayload => {
    const history = [...state.messages].map(msg => ({
      role: msg.role,
      parts: msg.parts
    }));
    history.push({ role: "user", parts: [{ type: "text", text }] });
    return {
      provider: options.provider,
      model: options.model,
      temperature: options.temperature,
      top_p: options.topP,
      max_tokens: options.maxTokens,
      reasoning_mode: options.reasoningMode,
      provider_options: {},
      messages: history
    };
  }, [options, state.messages]);

  const handleEvent = useCallback((event: StreamEvent) => {
    switch (event.type) {
      case "text.delta":
        appendAssistantDelta((event as any).text ?? "");
        break;
      case "reasoning.delta":
        appendReasoningDelta((event as any).reasoning ?? (event as any).text ?? "");
        break;
      case "reasoning.summary":
        setReasoningSummary((event as any).summary ?? "");
        break;
      case "tool.call":
        updateToolCall({
          id: event.tool_call.id,
          name: event.tool_call.name,
          input: event.tool_call.input,
          status: "pending"
        });
        break;
      case "tool.result":
        updateToolCall({
          id: event.tool_result.id,
          name: event.tool_result.name,
          input: {},
          status: event.tool_result.error ? "error" : "success",
          result: event.tool_result.result,
          error: event.tool_result.error
        });
        break;
      case "finish":
        finalizeAssistantMessage(
          event.usage
            ? {
                inputTokens: event.usage.input_tokens ?? 0,
                outputTokens: event.usage.output_tokens ?? 0,
                totalTokens: event.usage.total_tokens ?? 0,
                costUSD: event.usage.cost_usd ?? undefined
              }
            : undefined
        );
        break;
      case "error": {
        const message =
          typeof event.error === "string"
            ? event.error
            : event.error?.message ?? (event.ext && typeof event.ext["message"] === "string" ? (event.ext["message"] as string) : "stream error");
        if (process.env.NODE_ENV !== "production") {
          console.error("Stream error event", event);
        }
        emitError(message);
        break;
      }
      default:
        break;
    }
  }, [appendAssistantDelta, appendReasoningDelta, emitError, finalizeAssistantMessage, setReasoningSummary, updateToolCall]);

  const sendMessage = useCallback(async (text: string) => {
    if (!text.trim()) {
      return;
    }
    cancel();
    resetStreamingState();
    pushUserMessage(text);

    const payload = buildPayload(text);
    const controller = new AbortController();
    abortRef.current = controller;
    setIsStreaming(true);

    try {
      const response = await fetch(`${API_BASE}/api/chat/stream`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
        signal: controller.signal
      });

      if (!response.ok || !response.body) {
        throw new Error(`request failed: ${response.status}`);
      }

      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";

      while (true) {
        const { value, done } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        let boundary = buffer.indexOf("\n\n");
        while (boundary >= 0) {
          const chunk = buffer.slice(0, boundary);
          buffer = buffer.slice(boundary + 2);
          if (chunk.startsWith("data:")) {
            const json = chunk.slice(5).trim();
            if (json && json !== "[DONE]") {
              try {
                const event = JSON.parse(json) as StreamEvent;
                handleEvent(event);
              } catch (err) {
                console.error("failed to parse event", err, json);
              }
            }
          }
          boundary = buffer.indexOf("\n\n");
        }
      }
    } catch (err) {
      if ((err as Error).name !== "AbortError") {
        emitError((err as Error).message);
      }
    } finally {
      finalizeAssistantMessage();
      setIsStreaming(false);
      abortRef.current = null;
    }
  }, [buildPayload, cancel, emitError, finalizeAssistantMessage, handleEvent, pushUserMessage, resetStreamingState]);

  useEffect(() => () => cancel(), [cancel]);

  return useMemo(() => ({
    state,
    isStreaming,
    sendMessage,
    cancel,
    setOptions,
    options
  }), [state, isStreaming, sendMessage, cancel, setOptions, options]);
}

export async function fetchProviders(): Promise<ProviderOption[]> {
  const response = await fetch(`${API_BASE}/api/providers`);
  if (!response.ok) {
    throw new Error(`failed to fetch providers: ${response.status}`);
  }
  return response.json();
}
