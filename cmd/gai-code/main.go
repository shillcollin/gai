package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/providers/anthropic"
	"github.com/shillcollin/gai/providers/gemini"
	"github.com/shillcollin/gai/providers/groq"
	"github.com/shillcollin/gai/providers/openai"
	"github.com/shillcollin/gai/providers/xai"
	"github.com/shillcollin/gai/runner"
	"github.com/shillcollin/gai/sandbox"
	"github.com/shillcollin/gai/tools"
)

type providerEntry struct {
	Name    string
	Label   string
	Models  []string
	Default string
	Client  core.Provider
}

type conversation struct {
	reader       *bufio.Reader
	providers    []*providerEntry
	current      *providerEntry
	currentModel string
	runner       *runner.Runner
	tools        []core.ToolHandle
	messages     []core.Message
	turn         int
}

func main() {
	if err := loadDotEnv(); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "failed to load .env: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Init Observability
	shutdownObs, err := initObservability(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "observability init warning: %v\n", err)
	}
	defer func() {
		if shutdownObs != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = shutdownObs(shutdownCtx)
		}
	}()

	// Build Providers
	providers, err := buildProviders()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing providers: %v\n", err)
		os.Exit(1)
	}

	// Initialize Sandbox Manager (Local Backend)
	// Note: We need a context that persists for the lifecycle of the manager
	mgrCtx, mgrCancel := context.WithCancel(context.Background())
	defer mgrCancel()

	sandboxManager, err := sandbox.NewManager(mgrCtx, sandbox.ManagerOptions{
		DefaultRuntime: sandbox.RuntimeSpec{Backend: sandbox.BackendLocal},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing sandbox: %v\n", err)
		os.Exit(1)
	}
	defer sandboxManager.Close()

	// Create a persistent session for the agent
	session, err := sandboxManager.CreateSession(mgrCtx, sandbox.SessionSpec{
		Runtime: sandbox.RuntimeSpec{Backend: sandbox.BackendLocal},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating sandbox session: %v\n", err)
		os.Exit(1)
	}
	defer session.Close()

	// Initialize Conversation
	conv := &conversation{
		reader:       bufio.NewReader(os.Stdin),
		providers:    providers,
		current:      providers[0],
		currentModel: providers[0].Default,
		tools:        buildTools(session),
	}
	// Register session with runner for specialized handling if needed (e.g. metrics)
	// But here we bind tools directly to the session.
	conv.runner = newRunner(conv.current.Client, session)

	// System Prompt
	systemPrompt := `You are a skilled software engineer assistant, similar to Claude Code.
You have access to tools to read files, write files, list directories, and execute shell commands.
Your goal is to help the user with coding tasks, exploration, and debugging.
Always be concise and helpful. When asked to modify code, prefer reading the file first to understand the context.
`
	conv.messages = []core.Message{core.SystemMessage(systemPrompt)}

	fmt.Println("gai-code — A Vercel AI SDK style agent harness for Go")
	fmt.Printf("Current provider: %s (%s)\n", conv.current.Label, conv.currentModel)
	fmt.Println("Type 'exit' or Ctrl+C to quit.")
	fmt.Println()

	// Agent Loop
	for {
		fmt.Print("> ")
		text, err := conv.readLine()
		if err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Println()
				return
			}
			fmt.Fprintf(os.Stderr, "read error: %v\n", err)
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if text == "exit" || text == "quit" {
			return
		}
		if strings.HasPrefix(text, "/model") {
			parts := strings.Fields(text)
			if len(parts) > 1 {
				model := parts[1]
				// Find provider for model? Or just set it?
				// For now, assume user knows what they are doing or we search.
				// Let's look for a provider that supports this model.
				found := false
				for _, p := range conv.providers {
					for _, m := range p.Models {
						if m == model {
							conv.current = p
							conv.currentModel = model
							conv.runner = newRunner(p.Client, session)
							found = true
							fmt.Printf("Switched to %s (%s)\n", p.Label, model)
							break
						}
					}
					if found { break }
				}
				if !found {
					// Fallback: just set model on current provider
					conv.currentModel = model
					// Re-init runner? Not strictly needed if provider is same, but safer.
					// Actually runner doesn't hold model state, request does.
					// But newRunner might be useful if we want to change params.
					fmt.Printf("Switched model to %s (provider: %s)\n", model, conv.current.Label)
				}
			} else {
				fmt.Println("Available models:")
				for _, p := range conv.providers {
					fmt.Printf("  %s: %s\n", p.Label, strings.Join(p.Models, ", "))
				}
			}
			continue
		}

		if err := conv.processTurn(ctx, text); err != nil {
			fmt.Fprintf(os.Stderr, "turn error: %v\n", err)
		}
	}
}

func (c *conversation) readLine() (string, error) {
	line, err := c.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func (c *conversation) processTurn(ctx context.Context, userInput string) error {
	c.messages = append(c.messages, core.UserMessage(core.TextPart(userInput)))

	req := core.Request{
		Model:      c.currentModel,
		Messages:   append([]core.Message(nil), c.messages...),
		Tools:      c.tools,
		ToolChoice: core.ToolChoiceAuto,
		StopWhen:   core.NoMoreTools(),
	}

	fmt.Println() // Newline before streaming response

	stream, err := c.runner.StreamRequest(ctx, req)
	if err != nil {
		return err
	}
	defer stream.Close()

	textByStep := make(map[int]*strings.Builder)
	var lastStepID int

	// Track tool execution to reconstruct history
	type toolEvent struct {
		Call   core.ToolCall
		Result *core.ToolResult // Nil if not yet received
	}
	stepTools := make(map[int][]toolEvent)

	for event := range stream.Events() {
		if event.StepID > lastStepID {
			lastStepID = event.StepID
		}

		switch event.Type {
		case core.EventTextDelta:
			b := textByStep[event.StepID]
			if b == nil {
				b = &strings.Builder{}
				textByStep[event.StepID] = b
			}
			b.WriteString(event.TextDelta)
			fmt.Print(event.TextDelta) // Stream to stdout
		case core.EventToolCall:
			inputJSON, _ := json.Marshal(event.ToolCall.Input)
			fmt.Printf("\n[Tool Call: %s(%s)]\n", event.ToolCall.Name, inputJSON)

			// Track call
			events := stepTools[event.StepID]
			events = append(events, toolEvent{Call: event.ToolCall})
			stepTools[event.StepID] = events

		case core.EventToolResult:
			res := event.ToolResult
			if res.Error != "" {
				fmt.Printf("[Tool Error: %s]\n", res.Error)
			} else {
				outputJSON, _ := json.Marshal(res.Result)
				outStr := string(outputJSON)
				if len(outStr) > 200 {
					outStr = outStr[:200] + "..."
				}
				fmt.Printf("[Tool Result: %s]\n", outStr)
			}

			// Track result matching the call
			events := stepTools[event.StepID]
			for i := range events {
				if events[i].Call.ID == res.ID {
					r := res // Copy
					events[i].Result = &r
					break
				}
			}
			stepTools[event.StepID] = events
		}
	}

	if err := stream.Err(); err != nil && !errors.Is(err, core.ErrStreamClosed) {
		return err
	}

	// Reconstruct history for the next turn
	// Loop through steps in order
	for i := 1; i <= lastStepID; i++ {
		// 1. Assistant Text
		if b := textByStep[i]; b != nil && b.Len() > 0 {
			c.messages = append(c.messages, core.AssistantMessage(b.String()))
		}

		// 2. Tool Calls and Results
		if events, ok := stepTools[i]; ok && len(events) > 0 {
			// Add all calls in one Assistant message
			calls := make([]core.Part, 0, len(events))
			for _, ev := range events {
				calls = append(calls, ev.Call)
			}
			c.messages = append(c.messages, core.Message{Role: core.Assistant, Parts: calls})

			// Add results as separate User messages (or one message with multiple parts? Core supports list of parts)
			results := make([]core.Part, 0, len(events))
			for _, ev := range events {
				if ev.Result != nil {
					results = append(results, *ev.Result)
				}
			}
			if len(results) > 0 {
				c.messages = append(c.messages, core.Message{Role: core.User, Parts: results})
			}
		}
	}

	fmt.Println()
	return nil
}

func buildTools(session *sandbox.Session) []core.ToolHandle {
	// Register standardized sandbox tools

	// 1. run_command (using standardized SDK tool)
	// We need to inject the session into the tool execution metadata.
	// The runner handles this via WithSkill or we can use a closure wrapper?
	// Since tools are created here, we can close over 'session' if we write custom adapters,
	// OR we rely on the runner to inject "sandbox.session" into metadata if we use `runner.WithSkillAssets`.
	// However, we are manually building tools here.
	// `tools.NewSandboxCommand` expects "sandbox.session" in metadata.
	// So we need a way to ensure that metadata is present.
	// The Runner has `interceptors`. We can add an interceptor to inject the session!
	// OR, simpler: The Runner has `WithSkillAssets`? No, we aren't using a "Skill" object here, just raw tools.
	// Wait, `runner.WithSkill` binds a manager.
	// Let's look at `runner.go`: `prepareSkillSession` creates a session. We already have one.

	// Simple solution: Wrap the tool execution to inject session into context or metadata.
	// Or just implement the tool closure to use the session directly (since we have it in scope).

	// run_command adapter
	runCmd := tools.NewCoreAdapter(tools.New[tools.SandboxCommandInput, tools.SandboxCommandOutput](
		"run_command",
		"Execute a shell command.",
		func(ctx context.Context, in tools.SandboxCommandInput, meta core.ToolMeta) (tools.SandboxCommandOutput, error) {
			// Reuse the logic from sandbox_command.go but with our captured session
			timeout := time.Duration(in.TimeoutSeconds) * time.Second
			opts := sandbox.ExecOptions{
				Command: in.Args,
				Workdir: in.Workdir,
				Timeout: timeout,
			}
			if in.Stdin != "" {
				opts.Stdin = []byte(in.Stdin)
			}
			result, err := session.Exec(ctx, opts)
			if err != nil {
				return tools.SandboxCommandOutput{}, err
			}
			return tools.SandboxCommandOutput{
				ExitCode: result.ExitCode,
				Stdout:   result.Stdout,
				Stderr:   result.Stderr,
				Duration: result.Duration.Milliseconds(),
				Command:  result.Command,
			}, nil
		},
	))

	// read_file adapter
	type readFileInput struct {
		Path string `json:"path"`
	}
	type readFileOutput struct {
		Content string `json:"content"`
	}
	readFile := tools.NewCoreAdapter(tools.New[readFileInput, readFileOutput](
		"read_file",
		"Read file content.",
		func(ctx context.Context, in readFileInput, meta core.ToolMeta) (readFileOutput, error) {
			// Use 'cat' via session
			res, err := session.Exec(ctx, sandbox.ExecOptions{
				Command: []string{"cat", in.Path},
			})
			if err != nil {
				return readFileOutput{}, err
			}
			if res.ExitCode != 0 {
				return readFileOutput{}, fmt.Errorf("read failed: %s", res.Stderr)
			}
			return readFileOutput{Content: res.Stdout}, nil
		},
	))

	// write_file adapter
	type writeFileInput struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	type writeFileOutput struct {
		Success bool `json:"success"`
	}
	writeFile := tools.NewCoreAdapter(tools.New[writeFileInput, writeFileOutput](
		"write_file",
		"Write file content.",
		func(ctx context.Context, in writeFileInput, meta core.ToolMeta) (writeFileOutput, error) {
			// Use FileTemplate to write safely via manager
			_, err := session.Exec(ctx, sandbox.ExecOptions{
				Command: []string{"true"}, // No-op command, just side effect of template writing
				Templates: []sandbox.FileTemplate{{
					Path: in.Path,
					Contents: in.Content,
					Mode: 0644,
				}},
			})
			if err != nil {
				return writeFileOutput{}, err
			}
			return writeFileOutput{Success: true}, nil
		},
	))

	// list_dir adapter
	type listDirInput struct {
		Path string `json:"path"`
	}
	type listDirOutput struct {
		Entries []string `json:"entries"`
	}
	listDir := tools.NewCoreAdapter(tools.New[listDirInput, listDirOutput](
		"list_directory",
		"List directory entries.",
		func(ctx context.Context, in listDirInput, meta core.ToolMeta) (listDirOutput, error) {
			// Use 'ls -F' via session
			path := in.Path
			if path == "" {
				path = "."
			}
			res, err := session.Exec(ctx, sandbox.ExecOptions{
				Command: []string{"ls", "-1F", path},
			})
			if err != nil {
				return listDirOutput{}, err
			}
			if res.ExitCode != 0 {
				return listDirOutput{}, fmt.Errorf("ls failed: %s", res.Stderr)
			}
			entries := strings.Split(strings.TrimSpace(res.Stdout), "\n")
			return listDirOutput{Entries: entries}, nil
		},
	))

	return []core.ToolHandle{runCmd, readFile, writeFile, listDir}
}

func newRunner(p core.Provider, session *sandbox.Session) *runner.Runner {
	return runner.New(
		p,
		runner.WithMaxParallel(1), // Sequential for clarity in CLI usually
		runner.WithToolTimeout(60*time.Second),
		runner.WithOnToolError(runner.ToolErrorAppendAndContinue),
		// We don't use WithSkill because we are managing the session manually in buildTools
	)
}

// --- Boilerplate for Providers and Observability (copied/adapted from gai-cli) ---

func loadDotEnv() error {
	// Simplified
	data, err := os.ReadFile(".env")
	if err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				os.Setenv(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
			}
		}
	}
	return nil
}

func initObservability(ctx context.Context) (func(context.Context) error, error) {
	// Minimal placeholder
	return func(context.Context) error { return nil }, nil
}

func buildProviders() ([]*providerEntry, error) {
	providers := make([]*providerEntry, 0, 4)

	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		providers = append(providers, &providerEntry{
			Name: "openai", Label: "OpenAI", Models: []string{"gpt-4o"}, Default: "gpt-4o",
			Client: openai.New(openai.WithAPIKey(key)),
		})
	}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		providers = append(providers, &providerEntry{
			Name: "anthropic", Label: "Anthropic", Models: []string{"claude-3-7-sonnet-20250219"}, Default: "claude-3-7-sonnet-20250219",
			Client: anthropic.New(anthropic.WithAPIKey(key)),
		})
	}
	if key := os.Getenv("GOOGLE_API_KEY"); key != "" {
		providers = append(providers, &providerEntry{
			Name: "gemini", Label: "Gemini", Models: []string{"gemini-2.5-pro"}, Default: "gemini-2.5-pro",
			Client: gemini.New(gemini.WithAPIKey(key)),
		})
	}
	if key := os.Getenv("GROQ_API_KEY"); key != "" {
		providers = append(providers, &providerEntry{
			Name: "groq", Label: "Groq", Models: []string{"llama3-8b-8192"}, Default: "llama3-8b-8192",
			Client: groq.New(groq.WithAPIKey(key)),
		})
	}
	if key := os.Getenv("XAI_API_KEY"); key != "" {
		providers = append(providers, &providerEntry{
			Name: "xai", Label: "xAI", Models: []string{"grok-beta"}, Default: "grok-beta",
			Client: xai.New(xai.WithAPIKey(key)),
		})
	}

	if len(providers) == 0 {
		return nil, fmt.Errorf("no providers found (set API keys in .env)")
	}
	return providers, nil
}
