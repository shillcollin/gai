package tools

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/internal/jsonschema"
	"github.com/shillcollin/gai/sandbox"
	"github.com/shillcollin/gai/schema"
)

// SandboxCommandInput captures runtime configuration for sandbox command execution.
type SandboxCommandInput struct {
	Args           []string `json:"args,omitempty"`
	Stdin          string   `json:"stdin,omitempty"`
	Workdir        string   `json:"workdir,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty" minimum:"0" description:"Optional timeout for the command in seconds"`
}

// SandboxCommandOutput captures sandbox execution results.
type SandboxCommandOutput struct {
	ExitCode int      `json:"exit_code"`
	Stdout   string   `json:"stdout"`
	Stderr   string   `json:"stderr"`
	Duration int64    `json:"duration_ms"`
	Command  []string `json:"command"`
}

// NewSandboxCommand constructs a tool that executes commands inside the skill sandbox.
func NewSandboxCommand(name, description string, defaultArgs []string) core.ToolHandle {
	return &sandboxCommandTool{
		name:        name,
		description: description,
		defaultArgs: append([]string(nil), defaultArgs...),
	}
}

type sandboxCommandTool struct {
	name        string
	description string
	defaultArgs []string

	once         sync.Once
	inputSchema  *schema.Schema
	outputSchema *schema.Schema
}

func (t *sandboxCommandTool) Name() string        { return t.name }
func (t *sandboxCommandTool) Description() string { return t.description }

func (t *sandboxCommandTool) InputSchema() *schema.Schema {
	t.ensureSchemas()
	return t.inputSchema
}

func (t *sandboxCommandTool) OutputSchema() *schema.Schema {
	t.ensureSchemas()
	return t.outputSchema
}

func (t *sandboxCommandTool) ensureSchemas() {
	t.once.Do(func() {
		in, err := jsonschema.Derive[SandboxCommandInput]()
		if err != nil {
			panic(fmt.Sprintf("sandbox command input schema: %v", err))
		}
		out, err := jsonschema.Derive[SandboxCommandOutput]()
		if err != nil {
			panic(fmt.Sprintf("sandbox command output schema: %v", err))
		}
		t.inputSchema = in
		t.outputSchema = out
	})
}

func (t *sandboxCommandTool) Execute(ctx context.Context, input map[string]any, meta core.ToolMeta) (any, error) {
	var payload SandboxCommandInput
	if err := mapToStruct(input, &payload); err != nil {
		return nil, err
	}
	args := payload.Args
	if len(args) == 0 {
		args = append([]string(nil), t.defaultArgs...)
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("sandbox command %q requires args", t.name)
	}

	session, err := extractSandboxSession(meta.Metadata)
	if err != nil {
		return nil, err
	}

	timeout := time.Duration(payload.TimeoutSeconds) * time.Second
	opts := sandbox.ExecOptions{
		Command: args,
		Workdir: payload.Workdir,
		Timeout: timeout,
	}
	if payload.Stdin != "" {
		opts.Stdin = []byte(payload.Stdin)
	}

	result, err := session.Exec(ctx, opts)
	if err != nil {
		return nil, err
	}
	return SandboxCommandOutput{
		ExitCode: result.ExitCode,
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		Duration: result.Duration.Milliseconds(),
		Command:  args,
	}, nil
}

func extractSandboxSession(meta map[string]any) (*sandbox.Session, error) {
	if meta == nil {
		return nil, fmt.Errorf("sandbox session metadata missing")
	}
	value, ok := meta["sandbox.session"]
	if !ok || value == nil {
		return nil, fmt.Errorf("sandbox session metadata missing")
	}
	session, ok := value.(*sandbox.Session)
	if !ok {
		return nil, fmt.Errorf("sandbox session metadata has unexpected type %T", value)
	}
	return session, nil
}
