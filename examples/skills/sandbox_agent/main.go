package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/shillcollin/gai/agent"
	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/providers/anthropic"
	"github.com/shillcollin/gai/providers/groq"
	"github.com/shillcollin/gai/providers/openai"
	openairesponses "github.com/shillcollin/gai/providers/openai-responses"
	"github.com/shillcollin/gai/providers/xai"
	"github.com/shillcollin/gai/runner"
	"github.com/shillcollin/gai/sandbox"
	"github.com/shillcollin/gai/tools"
)

const (
	defaultBundlePath = "agents/python-sandbox"
	defaultAddr       = ":8080"
	// Default to gpt-5; this routes through the OpenAI Responses API.
	defaultModel = "gpt-5"
)

func main() {
	bundlePath := flag.String("bundle", defaultBundlePath, "Path to the agent bundle directory")
	skillNameFlag := flag.String("skill", "", "Skill name within the bundle (defaults to bundle's default skill)")
	addr := flag.String("addr", defaultAddr, "HTTP listen address (e.g. :8080)")
	modelFlag := flag.String("model", "", "Override model identifier (optional)")
	flag.Parse()

	if err := loadEnvFile(".env"); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("load .env: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	provider, providerModel, providerName, err := initProvider(*modelFlag)
	if err != nil {
		log.Fatalf("configure provider: %v", err)
	}

	bundle, err := agent.LoadBundle(*bundlePath)
	if err != nil {
		log.Fatalf("load bundle: %v", err)
	}
	skillName := strings.TrimSpace(*skillNameFlag)
	if skillName == "" {
		if bundle.Manifest.DefaultSkill == "" {
			log.Fatalf("bundle does not define a default skill; pass --skill")
		}
		skillName = bundle.Manifest.DefaultSkill
	}
	cfg, ok := bundle.SkillConfig(skillName)
	if !ok {
		log.Fatalf("skill %q not found in bundle", skillName)
	}
	skill := cfg.Skill

	manager, err := sandbox.NewManager(ctx, sandbox.ManagerOptions{})
	if err != nil {
		log.Fatalf("create sandbox manager: %v", err)
	}
	defer manager.Close()

	execTool := tools.NewSandboxCommand(
		"sandbox_exec",
		"Execute shell commands inside the skill sandbox",
		[]string{"/bin/sh", "-lc", ""},
	)

	assets := sandbox.SessionAssets{
		Workspace: cfg.Workspace,
		Mounts:    cfg.Mounts,
	}
	run := runner.New(provider,
		runner.WithSkillAssets(skill, manager, assets),
		runner.WithMaxParallel(4),
		runner.WithToolTimeout(60*time.Second),
	)

	s := &agentServer{
		bundle:      bundle,
		skillConfig: cfg,
		manager:     manager,
		runner:      run,
		execTool:    execTool,
		model:       providerModel,
		provider:    providerName,
		runtimeInfo: formatRuntimeInfo(bundle, cfg, providerName, providerModel),
		maxSteps:    bundle.Manifest.MaxSteps,
	}

	server := &http.Server{
		Addr:    *addr,
		Handler: s.routes(),
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("sandbox agent listening on %s (bundle=%s provider=%s model=%s skill=%s@%s)", *addr, bundle.Manifest.Name, providerName, providerModel, cfg.Skill.Manifest.Name, cfg.Skill.Manifest.Version)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("http server error: %v", err)
	}
}

type agentServer struct {
	bundle      *agent.Bundle
	skillConfig agent.SkillConfig
	manager     *sandbox.Manager
	runner      *runner.Runner
	execTool    core.ToolHandle
	model       string
	provider    string
	runtimeInfo string
	maxSteps    int
}

func (s *agentServer) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/run", s.handleRun)
	mux.HandleFunc("/skill", s.handleSkillInfo)
	return loggingMiddleware(mux)
}

func (s *agentServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}

func (s *agentServer) handleSkillInfo(w http.ResponseWriter, r *http.Request) {
	info := map[string]any{
		"bundle":       s.bundle.Manifest.Name,
		"name":         s.skillConfig.Skill.Manifest.Name,
		"version":      s.skillConfig.Skill.Manifest.Version,
		"summary":      s.skillConfig.Skill.Manifest.Summary,
		"instructions": s.skillConfig.Skill.Manifest.Instructions,
		"provider":     s.provider,
		"model":        s.model,
		"runtime":      s.runtimeInfo,
		"tools":        s.skillConfig.Skill.Tools(),
		"workspace":    s.skillConfig.Workspace,
		"mounts":       s.skillConfig.Mounts,
		"bundle_steps": s.bundle.Manifest.MaxSteps,
	}
	writeJSON(w, info, http.StatusOK)
}

func (s *agentServer) handleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Prompt == "" {
		http.Error(w, "prompt is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()

	stream, err := s.runner.StreamRequest(ctx, core.Request{
		Model:      s.model,
		Messages:   buildConversation(s.skillConfig.Skill.Manifest.Instructions, req.Prompt),
		Tools:      []core.ToolHandle{s.execTool},
		ToolChoice: core.ToolChoiceAuto,
		Metadata: map[string]any{
			"app":            "sandbox-demo",
			"bundle":         s.bundle.Manifest.Name,
			"bundle_version": s.bundle.Manifest.Version,
			"skill_name":     s.skillConfig.Skill.Manifest.Name,
			"skill_version":  s.skillConfig.Skill.Manifest.Version,
			"user_prompt":    req.Prompt,
		},
		StopWhen: core.MaxSteps(s.maxSteps),
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("runner error: %v", err), http.StatusInternalServerError)
		return
	}
	defer stream.Close()

	var (
		textBuilder strings.Builder
		toolMu      sync.Mutex
		toolEvents  = map[string]*toolTranscript{}
	)

	for event := range stream.Events() {
		switch event.Type {
		case core.EventTextDelta:
			textBuilder.WriteString(event.TextDelta)
		case core.EventToolCall:
			toolMu.Lock()
			rec := toolEvents[event.ToolCall.ID]
			if rec == nil {
				rec = &toolTranscript{}
				toolEvents[event.ToolCall.ID] = rec
			}
			rec.ToolCall = event.ToolCall
			rec.StepID = event.StepID
			toolMu.Unlock()
		case core.EventToolResult:
			toolMu.Lock()
			rec := toolEvents[event.ToolResult.ID]
			if rec == nil {
				rec = &toolTranscript{}
				toolEvents[event.ToolResult.ID] = rec
			}
			rec.ToolResult = event.ToolResult
			rec.StepID = event.StepID
			toolMu.Unlock()
		case core.EventError:
			if event.Error != nil {
				http.Error(w, fmt.Sprintf("stream error: %v", event.Error), http.StatusInternalServerError)
				return
			}
		}
	}
	if err := stream.Err(); err != nil && !errors.Is(err, core.ErrStreamClosed) {
		http.Error(w, fmt.Sprintf("stream error: %v", err), http.StatusInternalServerError)
		return
	}

	meta := stream.Meta()
	response := runResponse{
		Output:       strings.TrimSpace(textBuilder.String()),
		Usage:        meta.Usage,
		Provider:     s.provider,
		Model:        meta.Model,
		Bundle:       s.bundle.Manifest.Name,
		Skill:        s.skillConfig.Skill.Manifest.Name,
		SkillVersion: s.skillConfig.Skill.Manifest.Version,
		RequestedAt:  time.Now().UTC(),
		Warnings:     meta.Warnings,
	}
	for _, rec := range toolEvents {
		response.Tools = append(response.Tools, rec)
	}

	writeJSON(w, response, http.StatusOK)
}

type toolTranscript struct {
	StepID     int             `json:"step_id"`
	ToolCall   core.ToolCall   `json:"call"`
	ToolResult core.ToolResult `json:"result"`
}

type runResponse struct {
	Output       string            `json:"output"`
	Usage        core.Usage        `json:"usage"`
	Provider     string            `json:"provider"`
	Model        string            `json:"model"`
	Bundle       string            `json:"bundle"`
	Skill        string            `json:"skill"`
	SkillVersion string            `json:"skill_version"`
	RequestedAt  time.Time         `json:"requested_at"`
	Warnings     []core.Warning    `json:"warnings,omitempty"`
	Tools        []*toolTranscript `json:"tools,omitempty"`
}

func buildConversation(instructions, prompt string) []core.Message {
	messages := []core.Message{
		core.SystemMessage(strings.TrimSpace(instructions)),
		core.UserMessage(core.TextPart(prompt)),
	}
	return messages
}

func formatRuntimeInfo(bundle *agent.Bundle, cfg agent.SkillConfig, provider string, model string) string {
	rt := cfg.Skill.Manifest.Sandbox.Session.Runtime
	workspace := cfg.Workspace
	if workspace == "" {
		workspace = "(embedded)"
	}
	return fmt.Sprintf("bundle=%s provider=%s model=%s image=%s workdir=%s warm=%t workspace=%s", bundle.Manifest.Name, provider, model, rt.Image, rt.Workdir, cfg.Skill.Manifest.Sandbox.Warm, workspace)
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s (%v)", r.Method, r.URL.Path, time.Since(start))
	})
}

func writeJSON(w http.ResponseWriter, v any, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON error: %v", err)
	}
}

func loadEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if key != "" {
			_ = os.Setenv(key, value)
		}
	}
	return scanner.Err()
}

func initProvider(modelOverride string) (core.Provider, string, string, error) {
	// Prefer OpenAI Responses when targeting gpt-5 or when OPENAI_RESPONSES_API_KEY is present.
	if key := strings.TrimSpace(os.Getenv("OPENAI_RESPONSES_API_KEY")); key != "" {
		model := chooseModel(modelOverride, os.Getenv("OPENAI_RESPONSES_MODEL"), defaultModel)
		client := openairesponses.New(
			openairesponses.WithAPIKey(key),
			openairesponses.WithModel(model),
		)
		return client, model, "openai-responses", nil
	}
	if key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); key != "" {
		// If user requested gpt-5, route via Responses API using the standard OpenAI key.
		requested := chooseModel(modelOverride, os.Getenv("OPENAI_MODEL"), defaultModel)
		if strings.HasPrefix(strings.ToLower(requested), "gpt-5") {
			client := openairesponses.New(
				openairesponses.WithAPIKey(key),
				openairesponses.WithModel(requested),
			)
			return client, requested, "openai-responses", nil
		}
		client := openai.New(
			openai.WithAPIKey(key),
			openai.WithModel(requested),
		)
		return client, requested, "openai", nil
	}
	if key := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")); key != "" {
		model := chooseModel(modelOverride, os.Getenv("ANTHROPIC_MODEL"), "claude-3-7-sonnet-20250219")
		client := anthropic.New(
			anthropic.WithAPIKey(key),
			anthropic.WithModel(model),
		)
		return client, model, "anthropic", nil
	}
	if key := strings.TrimSpace(os.Getenv("GROQ_API_KEY")); key != "" {
		model := chooseModel(modelOverride, os.Getenv("GROQ_MODEL"), "llama3-70b-8192")
		client := groq.New(
			groq.WithAPIKey(key),
			groq.WithModel(model),
		)
		return client, model, "groq", nil
	}
	if key := strings.TrimSpace(os.Getenv("XAI_API_KEY")); key != "" {
		model := chooseModel(modelOverride, os.Getenv("XAI_MODEL"), "grok-4")
		client := xai.New(
			xai.WithAPIKey(key),
			xai.WithModel(model),
		)
		return client, model, "xai", nil
	}
	return nil, "", "", errors.New("no provider API key found; set OPENAI_API_KEY or other supported key in .env")
}

func chooseModel(values ...string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return defaultModel
}

const indexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <title>gai Sandbox Skill Demo</title>
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <style>
    body { font-family: system-ui, sans-serif; margin: 0; background: #0f172a; color: #e2e8f0; }
    header { padding: 24px; background: #1e293b; }
    main { padding: 24px; max-width: 960px; margin: 0 auto; }
    textarea { width: 100%; min-height: 120px; padding: 12px; border-radius: 8px; border: 1px solid #334155; background: #0f172a; color: inherit; }
    button { margin-top: 12px; padding: 12px 18px; background: #38bdf8; border: none; border-radius: 8px; color: #0f172a; font-weight: 600; cursor: pointer; }
    pre { background: #0b1120; padding: 16px; border-radius: 8px; overflow-x: auto; }
    .output { margin-top: 24px; padding: 20px; border-radius: 12px; background: #1e293b; border: 1px solid #334155; }
    .tool { margin-top: 16px; padding: 16px; border-radius: 12px; background: rgba(56, 189, 248, 0.1); }
    .tool h3 { margin-top: 0; }
  </style>
</head>
<body>
  <header>
    <h1>gai Sandbox Skill Demo</h1>
    <p>Send a task and watch the agent plan, execute sandbox commands, and respond.</p>
  </header>
  <main>
    <section>
      <form id="prompt-form">
        <label for="prompt">Prompt</label>
        <textarea id="prompt" name="prompt" placeholder="e.g. Create a hello.py script that prints the current time, run it, and show me the output."></textarea>
        <button type="submit">Run Skill</button>
      </form>
    </section>
    <section class="output" style="display:none" id="output-card">
      <h2>Assistant Response</h2>
      <pre id="assistant-output">(pending)</pre>
      <div id="tool-events"></div>
      <pre id="usage-info"></pre>
    </section>
  </main>
  <script>
    async function fetchSkillInfo() {
      const res = await fetch('/skill');
      if (!res.ok) return;
    const info = await res.json();
    const header = document.querySelector('header');
    const meta = document.createElement('p');
    meta.textContent = "Skill: " + info.name + "@" + info.version + " — Provider: " + info.provider + " — Model: " + info.model;
    header.appendChild(meta);
    }

    async function runPrompt(prompt) {
      const response = await fetch('/run', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ prompt })
      });
      if (!response.ok) {
        const text = await response.text();
        throw new Error(text || 'Request failed');
      }
      return response.json();
    }

    document.getElementById('prompt-form').addEventListener('submit', async (event) => {
      event.preventDefault();
      const outputCard = document.getElementById('output-card');
      const output = document.getElementById('assistant-output');
      const toolEvents = document.getElementById('tool-events');
      const usageInfo = document.getElementById('usage-info');

      const prompt = document.getElementById('prompt').value.trim();
      if (!prompt) return;

      outputCard.style.display = 'block';
      output.textContent = 'Thinking...';
      toolEvents.innerHTML = '';
      usageInfo.textContent = '';

      try {
        const data = await runPrompt(prompt);
        output.textContent = data.output || '(no response)';
        if (data.tools) {
          for (const tool of data.tools) {
            const div = document.createElement('div');
            div.className = 'tool';
            const callInput = JSON.stringify(tool.call?.input ?? {}, null, 2);
            const result = JSON.stringify(tool.result?.result ?? tool.result?.error ?? {}, null, 2);
            div.innerHTML =
              '<h3>Tool: ' + (tool.call?.name || '(unknown)') + '</h3>' +
              '<p><strong>Input:</strong></p>' +
              '<pre>' + callInput + '</pre>' +
              '<p><strong>Result:</strong></p>' +
              '<pre>' + result + '</pre>';
            toolEvents.appendChild(div);
          }
        }
        const usage = data.usage || {};
        usageInfo.textContent = "Usage: input=" + (usage.input_tokens ?? 0) + ", output=" + (usage.output_tokens ?? 0) + ", total=" + (usage.total_tokens ?? 0);
      } catch (err) {
        output.textContent = 'Error: ' + err.message;
      }
    });

    fetchSkillInfo();
  </script>
</body>
</html>`
