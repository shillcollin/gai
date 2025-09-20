// gai Normalized Stream Events v1 (TypeScript types)
// Derived from docs/schema/gai.events.v1.json

export type SchemaVersion = "gai.events.v1";

export interface PromptID { name: string; version?: string; fingerprint?: string }

export interface EventBase {
  schema: SchemaVersion;
  type: string;
  seq: number;
  ts: string; // RFC3339
  stream_id: string;
  request_id?: string;
  provider?: string;
  model?: string;
  trace_id?: string;
  span_id?: string;
  prompt_id?: PromptID;
  ext?: Record<string, unknown>;
}

export type StopReasonType =
  | "stop"
  | "length"
  | "content_filter"
  | "tool_calls_exhausted"
  | "max_steps"
  | "aborted"
  | "error";

export interface StopReason { type: StopReasonType; description?: string; details?: Record<string, unknown> }
export interface Usage { input_tokens?: number; output_tokens?: number; reasoning_tokens?: number; total_tokens?: number; cost_usd?: number }

export interface StartEvent extends EventBase {
  type: "start";
  capabilities?: string[];
  policies?: { reasoning_visibility?: "none" | "summary" | "raw"; [k: string]: unknown };
}

export interface TextDeltaEvent extends EventBase {
  type: "text.delta";
  text: string;
  partial?: boolean;
  cursor?: { chars_emitted: number };
  step_id?: number;
  role?: "assistant";
}

export interface ReasoningDeltaEvent extends EventBase {
  type: "reasoning.delta";
  text: string;
  partial?: boolean;
  cursor?: { tokens_emitted: number };
  step_id?: number;
  visibility?: "raw";
  notice?: string;
}

export interface ReasoningSummaryEvent extends EventBase {
  type: "reasoning.summary";
  summary: string;
  steps?: string[];
  confidence?: number; // 0..1
  tokens?: number;
  method?: "model" | "heuristic" | "provider";
  step_id?: number;
  visibility?: "summary";
  signatures?: Array<{ provider: string; type: string; value: string }>;
}

export interface AudioDeltaEvent extends EventBase {
  type: "audio.delta";
  audio_b64: string;
  format: string;
  partial?: boolean;
  step_id?: number;
}

export interface ToolCall { call_id: string; name: string; input: Record<string, unknown>; timeout_ms?: number; parallel_group?: number }
export interface ToolCallEvent extends EventBase { type: "tool.call"; tool_call: ToolCall; step_id?: number }

export interface ToolResult { call_id: string; ok?: boolean; result?: unknown; error?: ErrorObject; duration_ms?: number }
export interface ToolResultEvent extends EventBase { type: "tool.result"; tool_result: ToolResult; step_id?: number }

export interface Citation { uri: string; title?: string; snippet?: string; start?: number; end?: number; score?: number }
export interface CitationsEvent extends EventBase { type: "citations"; citations: Citation[]; step_id?: number }

export interface Safety { category: "harassment" | "hate" | "sexual" | "dangerous" | "selfharm" | "other"; action: "allow" | "block" | "redact" | "safe_completion"; score?: number; target?: "input" | "output"; start?: number; end?: number }
export interface SafetyEvent extends EventBase { type: "safety"; safety: Safety; step_id?: number }

export interface StepStartEvent extends EventBase { type: "step.start"; step_id: number }
export interface StepFinishEvent extends EventBase { type: "step.finish"; step_id: number; duration_ms?: number; tool_calls?: number; text_len?: number }

export interface FinishEvent extends EventBase { type: "finish"; finish_reason: StopReason; usage?: Usage; final?: true }

export type ErrorCode =
  | "rate_limited"
  | "content_filtered"
  | "bad_request"
  | "transient"
  | "provider_error"
  | "timeout"
  | "canceled"
  | "tool_error"
  | "internal";

export interface ErrorObject { code: ErrorCode; message: string; status?: number; retryable?: boolean; details?: Record<string, unknown> }
export interface ErrorEvent extends EventBase { type: "error"; error: ErrorObject; step_id?: number }

export type GaiEvent =
  | StartEvent
  | TextDeltaEvent
  | ReasoningDeltaEvent
  | ReasoningSummaryEvent
  | AudioDeltaEvent
  | ToolCallEvent
  | ToolResultEvent
  | CitationsEvent
  | SafetyEvent
  | StepStartEvent
  | StepFinishEvent
  | FinishEvent
  | ErrorEvent;
