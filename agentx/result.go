package agentx

import (
	"encoding/json"

	"github.com/shillcollin/gai/core"
)

// DeliverableRef identifies an artifact produced during a task run.
type DeliverableRef struct {
	Path   string `json:"path"`
	Kind   string `json:"kind,omitempty"`
	Title  string `json:"title,omitempty"`
	MIME   string `json:"mime,omitempty"`
	Size   int64  `json:"size,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

// StepReport summarises the outcome of a single step in the loop.
type StepReport struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind,omitempty"`
	Iteration int    `json:"iteration,omitempty"`
	Status    string `json:"status"`

	StartedAt int64 `json:"started_at,omitempty"`
	EndedAt   int64 `json:"ended_at,omitempty"`

	ToolsAllowed     []string             `json:"tools_allowed,omitempty"`
	ToolsUsed        []string             `json:"tools_used,omitempty"`
	ToolCalls        []core.ToolExecution `json:"tool_calls,omitempty"`
	ContextRead      []string             `json:"context_read,omitempty"`
	ArtifactsWritten []DeliverableRef     `json:"artifacts_written,omitempty"`
	OutputRef        string               `json:"output_ref,omitempty"`
	OutputJSON       json.RawMessage      `json:"output_json,omitempty"`
	SchemaID         string               `json:"schema_id,omitempty"`
	Usage            core.Usage           `json:"usage"`
	Error            string               `json:"error,omitempty"`
	Ext              map[string]any       `json:"ext,omitempty"`
}

// DoTaskResult is the top-level return value from DoTask.
type DoTaskResult struct {
	AgentID       string `json:"agent_id"`
	TaskID        string `json:"task_id"`
	LoopName      string `json:"loop_name"`
	LoopVersion   string `json:"loop_version"`
	LoopSignature string `json:"loop_signature,omitempty"`

	ProgressDir  string           `json:"progress_dir"`
	Deliverables []DeliverableRef `json:"deliverables,omitempty"`
	OutputText   string           `json:"output_text,omitempty"`

	Usage        core.Usage      `json:"usage"`
	FinishReason core.StopReason `json:"finish_reason"`
	Warnings     []core.Warning  `json:"warnings,omitempty"`

	Steps       []StepReport `json:"steps,omitempty"`
	StartedAt   int64        `json:"started_at"`
	CompletedAt int64        `json:"completed_at"`

	Ext map[string]any `json:"ext,omitempty"`
}
