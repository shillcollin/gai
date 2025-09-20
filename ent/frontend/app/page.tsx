"use client";

import { FormEvent, useEffect, useMemo, useState } from "react";
import { fetchProviders, useChatStream } from "./lib/hooks/useChatStream";
import type { ChatMessage, ProviderOption } from "./lib/types";

export default function ChatPage(): JSX.Element {
  const [providers, setProviders] = useState<ProviderOption[]>([]);
  const [input, setInput] = useState("");
  const chat = useChatStream();

  useEffect(() => {
    fetchProviders()
      .then(setProviders)
      .catch(err => console.error("failed to load providers", err));
  }, []);

  const activeProvider = useMemo(() => providers.find(p => p.name === chat.options.provider), [providers, chat.options.provider]);
  const models = activeProvider?.models ?? [chat.options.model];

  const allMessages: ChatMessage[] = useMemo(() => {
    const merged = [...chat.state.messages];
    if (chat.state.pendingAssistant) {
      merged.push(chat.state.pendingAssistant);
    }
    return merged;
  }, [chat.state.messages, chat.state.pendingAssistant]);

  const onSubmit = (event: FormEvent) => {
    event.preventDefault();
    const trimmed = input.trim();
    if (!trimmed) return;
    setInput("");
    void chat.sendMessage(trimmed);
  };

  return (
    <div className="chat-shell">
      <aside className="sidebar">
        <div>
          <h1>gai Chat</h1>
          <p style={{ color: "rgba(255,255,255,0.6)", margin: 0 }}>Provider-agnostic assistant</p>
        </div>

        <section>
          <label htmlFor="provider">Provider</label>
          <select
            id="provider"
            value={chat.options.provider}
            onChange={e => {
              const nextProvider = e.target.value;
              chat.setOptions({ provider: nextProvider });
              const next = providers.find(p => p.name === nextProvider);
              if (next) {
                chat.setOptions({ model: next.default_model });
              }
            }}
          >
            {providers.map(provider => (
              <option key={provider.name} value={provider.name}>
                {provider.label}
              </option>
            ))}
          </select>
        </section>

        <section>
          <label htmlFor="model">Model</label>
          <select
            id="model"
            value={chat.options.model}
            onChange={e => chat.setOptions({ model: e.target.value })}
          >
            {models.map(model => (
              <option key={model} value={model}>
                {model}
              </option>
            ))}
          </select>
        </section>

        <section>
          <label htmlFor="temperature">Temperature ({chat.options.temperature.toFixed(1)})</label>
          <input
            id="temperature"
            type="range"
            min="0"
            max="1"
            step="0.1"
            value={chat.options.temperature}
            onChange={e => chat.setOptions({ temperature: parseFloat(e.target.value) })}
          />
        </section>

        <section>
          <label htmlFor="reasoning">Reasoning visibility</label>
          <select
            id="reasoning"
            value={chat.options.reasoningMode}
            onChange={e => chat.setOptions({ reasoningMode: e.target.value as "none" | "summary" | "raw" })}
          >
            <option value="none">Hidden</option>
            <option value="summary">Summary</option>
            <option value="raw">Raw</option>
          </select>
        </section>
      </aside>

      <main className="main-panel">
        <header className="header">
          <div className="streaming-indicator" style={{ visibility: chat.isStreaming ? "visible" : "hidden" }}>
            <span className="dot" />
            <span className="dot" />
            <span className="dot" />
            <span>Streaming</span>
          </div>
          {chat.state.usage && (
            <div style={{ fontSize: "13px", color: "#475569" }}>
              Input: {chat.state.usage.inputTokens} tokens · Output: {chat.state.usage.outputTokens} tokens · Total: {chat.state.usage.totalTokens}
            </div>
          )}
        </header>

        <section className="message-log">
          {allMessages.length === 0 && (
            <div style={{ textAlign: "center", color: "#64748b" }}>
              Ask anything and watch the assistant stream responses in real time.
            </div>
          )}

          {allMessages.map(message => (
            <MessageBubble key={message.id} message={message} />
          ))}

          {chat.state.error && <div className="error-banner">{chat.state.error}</div>}
        </section>

        <form className="input-panel" onSubmit={onSubmit}>
          <textarea
            placeholder="Send a message..."
            value={input}
            onChange={e => setInput(e.target.value)}
            disabled={chat.isStreaming}
          />
          <div className="input-actions">
            <div className="streaming-indicator" style={{ visibility: chat.isStreaming ? "visible" : "hidden" }}>
              <span className="dot" />
              <span className="dot" />
              <span className="dot" />
              <span>Generating response...</span>
            </div>
            <div className="controls">
              {chat.isStreaming && (
                <button type="button" className="button button-secondary" onClick={chat.cancel}>
                  Stop
                </button>
              )}
              <button type="submit" className="button button-primary" disabled={chat.isStreaming || !input.trim()}>
                Send
              </button>
            </div>
          </div>
        </form>
      </main>
    </div>
  );
}

function MessageBubble({ message }: { message: ChatMessage }): JSX.Element {
  const isAssistant = message.role === "assistant";
  const initials = isAssistant ? "AI" : "You";

  return (
    <div className={`message ${isAssistant ? "assistant" : "user"}`}>
      <div className="avatar">{initials}</div>
      <div className="content">
        <div className="role">{isAssistant ? "Assistant" : "User"}</div>
        {message.parts.map((part, idx) => (
          <p key={idx} style={{ margin: "8px 0" }}>
            {part.text}
          </p>
        ))}

        {message.reasoning && message.reasoning.length > 0 && (
          <div className="reasoning-card">
            <h4>Reasoning</h4>
            <div>
              {message.reasoning.map((chunk, idx) => (
                <span className="reasoning-token" key={idx}>
                  {chunk}
                </span>
              ))}
            </div>
            {message.reasoningSummary && <p style={{ marginTop: 8 }}>{message.reasoningSummary}</p>}
          </div>
        )}

        {message.toolCalls && message.toolCalls.length > 0 && (
          <div className="tool-call-card">
            <h4>Tools</h4>
            <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
              {message.toolCalls.map(call => (
                <div key={call.id} className={`tool-result-card ${call.status === "error" ? "error" : call.status === "success" ? "success" : "pending"}`}>
                  <div style={{ fontWeight: 600 }}>{call.name}</div>
                  <div style={{ fontSize: 12, opacity: 0.8 }}>Input: {JSON.stringify(call.input)}</div>
                  {call.status === "success" && <div>Result: {JSON.stringify(call.result)}</div>}
                  {call.status === "error" && <div>Error: {call.error}</div>}
                  {call.status === "pending" && <div>Running...</div>}
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
