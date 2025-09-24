"use client"

import { useEffect, useMemo, useState } from "react"

const API_BASE = process.env.NEXT_PUBLIC_GAI_API_BASE ?? "http://localhost:8080"

type ProviderInfo = {
  id: string
  label: string
  default_model: string
  models: string[]
  capabilities: {
    images: boolean
    audio: boolean
    video: boolean
    reasoning: boolean
  }
  tools: string[]
}

type Part =
  | { type: "text"; text: string }
  | { type: "image"; dataUrl: string; mime: string }

type ConversationMessage = {
  id: string
  role: "system" | "user" | "assistant"
  parts: Part[]
  steps?: StepDTO[]
  warnings?: WarningDTO[]
  usage?: UsageDTO
}

type StepDTO = {
  number: number
  text: string
  model: string
  duration_ms: number
  tool_calls: ToolCallDTO[]
}

type ToolCallDTO = {
  id: string
  name: string
  input: Record<string, unknown>
  result?: unknown
  error?: string
  duration_ms: number
}

type WarningDTO = {
  code: string
  field?: string
  message: string
}

type UsageDTO = {
  input_tokens: number
  output_tokens: number
  total_tokens: number
  reasoning_tokens?: number
}

type ChatResponse = {
  id: string
  text: string
  json?: unknown
  model: string
  usage: UsageDTO
  finish_reason: { type: string; description?: string }
  steps: StepDTO[]
  warnings?: WarningDTO[]
}

type ApiMessage = {
  role: string
  parts: { type: string; text?: string; data?: string; mime?: string }[]
}

type ChatRequest = {
  provider: string
  model?: string
  mode?: string
  messages: ApiMessage[]
  temperature?: number
  max_output_tokens?: number
  tool_choice?: string
  tools?: string[]
}

const SYSTEM_PROMPT = "You are a helpful assistant powered by the GAI SDK." as const

export default function Page() {
  const [providers, setProviders] = useState<ProviderInfo[]>([])
  const [providerId, setProviderId] = useState<string>("")
  const [model, setModel] = useState<string>("")
  const [messages, setMessages] = useState<ConversationMessage[]>([{
    id: crypto.randomUUID(),
    role: "system",
    parts: [{ type: "text", text: SYSTEM_PROMPT }],
  }])
  const [input, setInput] = useState("")
  const [mode, setMode] = useState<"text" | "json">("text")
  const [temperature, setTemperature] = useState(0.7)
  const [maxOutputTokens, setMaxOutputTokens] = useState(1024)
  const [selectedTools, setSelectedTools] = useState<Record<string, boolean>>({})
  const [selectedImage, setSelectedImage] = useState<{ dataUrl: string; mime: string } | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    fetch(`${API_BASE}/api/providers`)
      .then((res) => res.json())
      .then((data: ProviderInfo[]) => {
        setProviders(data)
        if (data.length > 0) {
          setProviderId((prev) => prev || data[0].id)
          setModel(data[0].default_model)
        }
      })
      .catch((err) => {
        console.error("Failed to load providers", err)
        setError("Failed to load providers. Is the API server running?")
      })
  }, [])

  useEffect(() => {
    const provider = providers.find((p) => p.id === providerId)
    if (provider) {
      setModel(provider.default_model)
      const defaults: Record<string, boolean> = {}
      provider.tools.forEach((tool) => {
        defaults[tool] = true
      })
      setSelectedTools(defaults)
    }
  }, [providerId, providers])

  const availableTools = useMemo(() => {
    const provider = providers.find((p) => p.id === providerId)
    return provider?.tools ?? []
  }, [providers, providerId])

  const handleImageChange = async (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]
    if (!file) {
      setSelectedImage(null)
      return
    }
    const dataUrl = await fileToDataUrl(file)
    setSelectedImage({ dataUrl, mime: file.type || "image/png" })
  }

  const handleSubmit = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!input.trim() && !selectedImage) {
      return
    }

    setLoading(true)
    setError(null)

    const userMessage: ConversationMessage = {
      id: crypto.randomUUID(),
      role: "user",
      parts: [
        ...(input.trim() ? [{ type: "text", text: input.trim() } as Part] : []),
        ...(selectedImage ? [{ type: "image", dataUrl: selectedImage.dataUrl, mime: selectedImage.mime } as Part] : []),
      ],
    }

    const nextMessages = [...messages, userMessage]
    setMessages(nextMessages)
    setInput("")
    setSelectedImage(null)

    const request: ChatRequest = {
      provider: providerId,
      model,
      mode,
      temperature,
      max_output_tokens: maxOutputTokens,
      messages: buildApiMessages(nextMessages),
      tools: Object.entries(selectedTools).filter(([_, enabled]) => enabled).map(([name]) => name),
      tool_choice: availableTools.length > 0 ? "auto" : "none",
    }

    try {
      const res = await fetch(`${API_BASE}/api/chat`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(request),
      })

      if (!res.ok) {
        const payload = await res.json().catch(() => ({}))
        throw new Error(payload.error || `Request failed with status ${res.status}`)
      }

      const data: ChatResponse = await res.json()
      const parts: Part[] = []
      if (typeof data.text === "string" && data.text.trim()) {
        parts.push({ type: "text", text: data.text.trim() })
      }
      if (data.json !== undefined) {
        const formatted = JSON.stringify(data.json, null, 2)
        parts.push({ type: "text", text: formatted })
      }
      const assistantMessage: ConversationMessage = {
        id: data.id,
        role: "assistant",
        parts: parts.length > 0 ? parts : [{ type: "text", text: "(no response)" }],
        steps: data.steps,
        warnings: data.warnings,
        usage: data.usage,
      }
      setMessages((prev) => [...prev, assistantMessage])
    } catch (err) {
      console.error(err)
      setError(err instanceof Error ? err.message : "unexpected error")
    } finally {
      setLoading(false)
    }
  }

  return (
    <main>
      <header>
        <span className="badge">GAI SDK</span>
        <h1>Build with OpenAI, Anthropic, and Gemini</h1>
        <p>
          This playground uses the Go-first GAI SDK to orchestrate multimodal prompts, tool calls, and structured output across providers.
        </p>
      </header>

      {error && (
        <div className="error-banner">{error}</div>
      )}

      <section className="card">
        <form className="control-grid" onSubmit={handleSubmit}>
          <label>
            Provider
            <select value={providerId} onChange={(e) => setProviderId(e.target.value)}>
              {providers.map((provider) => (
                <option key={provider.id} value={provider.id}>
                  {provider.label}
                </option>
              ))}
            </select>
          </label>

          <label>
            Model
            <select value={model} onChange={(e) => setModel(e.target.value)}>
              {providers
                .find((p) => p.id === providerId)?.models.map((model) => (
                  <option key={model} value={model}>
                    {model}
                  </option>
                ))}
            </select>
          </label>

          <label>
            Temperature
            <input
              type="number"
              min={0}
              max={2}
              step={0.1}
              value={temperature}
              onChange={(e) => setTemperature(parseFloat(e.target.value))}
            />
          </label>

          <label>
            Max Output Tokens
            <input
              type="number"
              min={32}
              max={4096}
              value={maxOutputTokens}
              onChange={(e) => setMaxOutputTokens(parseInt(e.target.value))}
            />
          </label>

          <label>
            Response Mode
            <select value={mode} onChange={(e) => setMode(e.target.value as "text" | "json")}> 
              <option value="text">Conversational</option>
              <option value="json">Structured JSON</option>
            </select>
          </label>

          <label>
            Attach Image
            <input type="file" accept="image/*" onChange={handleImageChange} />
          </label>

          {availableTools.length > 0 && (
            <div className="toggle-row" style={{ gridColumn: "1 / -1" }}>
              {availableTools.map((tool) => (
                <label key={tool}>
                  <input
                    type="checkbox"
                    checked={Boolean(selectedTools[tool])}
                    onChange={(e) =>
                      setSelectedTools((prev) => ({ ...prev, [tool]: e.target.checked }))
                    }
                  />
                  {tool}
                </label>
              ))}
            </div>
          )}

          <label style={{ gridColumn: "1 / -1" }}>
            Message
            <textarea
              value={input}
              onChange={(e) => setInput(e.target.value)}
              placeholder="Ask anything, or request structured JSON."
            />
          </label>

          {selectedImage && (
            <div className="selected-image" style={{ gridColumn: "1 / -1" }}>
              <img src={selectedImage.dataUrl} alt="attachment preview" className="preview" style={{ maxWidth: "180px", borderRadius: "12px" }} />
            </div>
          )}

          <button className="primary" type="submit" disabled={loading}>
            {loading ? "Generating…" : "Send"}
          </button>
        </form>
      </section>

      <section className="card">
        <div className="chat-thread">
          {messages.filter((msg) => msg.role !== "system" || msg.parts.some(part => part.type === "text" && part.text !== SYSTEM_PROMPT)).map((message) => (
            <article key={message.id} className="message">
              <h3>{message.role === "assistant" ? "Assistant" : "You"}</h3>
              {message.parts.map((part, index) => {
                if (part.type === "text") {
                  return <p key={index} style={{ whiteSpace: "pre-wrap" }}>{part.text}</p>
                }
                return <img key={index} className="preview" src={part.dataUrl} alt="user upload" />
              })}

              {message.steps && message.steps.length > 0 && (
                <div>
                  {message.steps.map((step) => (
                    <div key={step.number} className="tool-call">
                      <strong>Step {step.number}</strong>{" "}
                      <span className="badge">{step.model}</span>
                      {step.tool_calls.map((call) => (
                        <details key={call.id} style={{ marginTop: "0.5rem" }}>
                          <summary>{call.name}</summary>
                          <pre>{JSON.stringify({ input: call.input, result: call.result, error: call.error }, null, 2)}</pre>
                        </details>
                      ))}
                    </div>
                  ))}
                </div>
              )}

              {message.warnings && message.warnings.length > 0 && (
                <ul className="warning-list">
                  {message.warnings.map((warning, idx) => (
                    <li key={idx}>{warning.message}</li>
                  ))}
                </ul>
              )}

              {message.usage && (
                <div className="usage">
                  tokens: in {message.usage.input_tokens} / out {message.usage.output_tokens} / total {message.usage.total_tokens}
                </div>
              )}
            </article>
          ))}
        </div>
      </section>
    </main>
  )
}

function buildApiMessages(messages: ConversationMessage[]): ApiMessage[] {
  return messages.map((msg) => ({
    role: msg.role,
    parts: msg.parts.map((part) => {
      if (part.type === "text") {
        return { type: "text", text: part.text }
      }
      const [header, base64] = part.dataUrl.split(",", 2)
      const mime = part.mime || header.replace("data:", "").replace(";base64", "")
      const data = base64 || part.dataUrl
      return { type: "image_base64", data, mime }
    }),
  }))
}

function hasTool(set: Record<string, boolean>, name: string) {
  return !!set[name]
}

function fileToDataUrl(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => {
      if (typeof reader.result === "string") {
        resolve(reader.result)
      } else {
        reject(new Error("failed to read file"))
      }
    }
    reader.onerror = () => reject(reader.error || new Error("failed to read file"))
    reader.readAsDataURL(file)
  })
}
