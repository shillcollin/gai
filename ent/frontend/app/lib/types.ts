export type StreamEventType =
  | "start"
  | "text.delta"
  | "reasoning.delta"
  | "reasoning.summary"
  | "tool.call"
  | "tool.result"
  | "finish"
  | "error";

export interface StreamEventBase {
  schema: string;
  type: StreamEventType;
  seq: number;
  ts: string;
  provider?: string;
  model?: string;
  stream_id?: string;
  ext?: Record<string, unknown>;
}

export interface TextDeltaEvent extends StreamEventBase {
  type: "text.delta";
  text: string;
}

export interface ReasoningDeltaEvent extends StreamEventBase {
  type: "reasoning.delta";
  reasoning?: string;
  text?: string;
}

export interface ReasoningSummaryEvent extends StreamEventBase {
  type: "reasoning.summary";
  summary: string;
}

export interface ToolCallPayload {
  id: string;
  name: string;
  input: Record<string, unknown>;
}

export interface ToolCallEvent extends StreamEventBase {
  type: "tool.call";
  tool_call: ToolCallPayload;
}

export interface ToolResultPayload {
  id: string;
  name: string;
  result?: unknown;
  error?: string;
}

export interface ToolResultEvent extends StreamEventBase {
  type: "tool.result";
  tool_result: ToolResultPayload;
}

export interface FinishEvent extends StreamEventBase {
  type: "finish";
  finish_reason?: {
    type: string;
    description?: string;
  };
  usage?: UsageEvent;
}

export interface ErrorEvent extends StreamEventBase {
  type: "error";
  error?: {
    code?: string;
    message?: string;
  } | string;
}

export interface UsageEvent {
  input_tokens?: number;
  output_tokens?: number;
  total_tokens?: number;
  cost_usd?: number;
}

export type StreamEvent =
  | TextDeltaEvent
  | ReasoningDeltaEvent
  | ReasoningSummaryEvent
  | ToolCallEvent
  | ToolResultEvent
  | FinishEvent
  | ErrorEvent
  | StreamEventBase;

export type Role = "system" | "user" | "assistant";

export interface ChatPart {
  type: "text";
  text: string;
}

export interface ChatMessage {
  id: string;
  role: Role;
  parts: ChatPart[];
  reasoning?: string[];
  reasoningSummary?: string;
  toolCalls?: ToolCallState[];
}

export interface ToolCallState {
  id: string;
  name: string;
  input: Record<string, unknown>;
  status: "pending" | "success" | "error";
  result?: unknown;
  error?: string;
}

export interface ProviderOption {
  name: string;
  label: string;
  models: string[];
  default_model: string;
}

export interface ChatRequestPayload {
  provider: string;
  model: string;
  temperature: number;
  top_p: number;
  max_tokens: number;
  reasoning_mode: string;
  provider_options?: Record<string, unknown>;
  messages: Array<{
    role: Role;
    parts: ChatPart[];
  }>;
}

export interface UsageSummary {
  inputTokens: number;
  outputTokens: number;
  totalTokens: number;
  costUSD?: number;
}
