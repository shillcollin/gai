package main

import (
	"context"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/shillcollin/gai/agentx"
	httpapi "github.com/shillcollin/gai/agentx/httpapi"
	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/providers/openai"
	"github.com/shillcollin/gai/runner"
	"github.com/shillcollin/gai/sandbox"
	"github.com/shillcollin/gai/skills"
	"github.com/shillcollin/gai/tools"
)

const (
	defaultGoal = "Research the beliefs and history of the Church of Jesus Christ of Latter-day Saints, produce a structured report, and generate a downloadable PDF using Python."
)

func main() {
	mode := flag.String("mode", "run", "Mode: run the task once (run) or launch the approvals/events web UI (serve)")
	addr := flag.String("addr", ":8080", "HTTP listen address for --mode=serve")
	rootFlag := flag.String("root", "", "Task root directory (defaults to AGENT_TASK_ROOT or examples/demo/lds_report)")
	goalFlag := flag.String("goal", "", "Override task goal when --mode=run")
	flag.Parse()

	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable required")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	root, err := resolveTaskRoot(*rootFlag)
	if err != nil {
		log.Fatalf("resolve task root: %v", err)
	}

	switch strings.ToLower(strings.TrimSpace(*mode)) {
	case "run":
		goal := strings.TrimSpace(*goalFlag)
		if goal == "" {
			goal = defaultGoal
		}
		if err := runOnce(ctx, apiKey, root, goal); err != nil {
			log.Fatalf("demo run failed: %v", err)
		}
	case "serve":
		if err := serve(ctx, apiKey, root, *addr); err != nil {
			log.Fatalf("demo server error: %v", err)
		}
	default:
		log.Fatalf("unknown mode %q (expected run or serve)", *mode)
	}
}

func runOnce(ctx context.Context, apiKey, root, goal string) error {
	manager, err := sandbox.NewManager(ctx, sandbox.ManagerOptions{})
	if err != nil {
		return fmt.Errorf("sandbox manager: %w", err)
	}
	defer manager.Close()

	agent, err := newDemoAgent(apiKey, manager)
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}

	spec := agentx.TaskSpec{Goal: goal}

	result, err := agent.DoTask(ctx, root, spec)
	if err != nil {
		return fmt.Errorf("agent failed: %w", err)
	}

	fmt.Printf("Finished with reason %s\n", result.FinishReason.Type)
	fmt.Printf("Progress dir: %s\n", result.ProgressDir)
	if len(result.Deliverables) > 0 {
		fmt.Println("Deliverables:")
		for _, d := range result.Deliverables {
			fmt.Printf("  - %s (%s)\n", d.Path, d.Kind)
		}
	}
	if result.OutputText != "" {
		fmt.Println("Summary:")
		fmt.Println(result.OutputText)
	}
	return nil
}

func serve(ctx context.Context, apiKey, root, addr string) error {
	manager, err := sandbox.NewManager(ctx, sandbox.ManagerOptions{})
	if err != nil {
		return fmt.Errorf("sandbox manager: %w", err)
	}
	defer manager.Close()

	baseDir := filepath.Dir(root)
	server := httpapi.NewServer(baseDir, func() (*agentx.Agent, error) {
		return newDemoAgent(apiKey, manager)
	})

	mux := http.NewServeMux()
	server.Register(mux)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		escapedRoot := template.HTMLEscapeString(root)
		rootParam := url.QueryEscape(root)
		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>LDS Research Demo Control</title>
  <style>
    body { font-family: sans-serif; margin: 2rem; line-height: 1.5; }
    code { background: #f4f4f4; padding: 0.2rem 0.4rem; border-radius: 4px; }
    ul { margin-top: 1rem; }
  </style>
</head>
<body>
  <h1>LDS Research Demo</h1>
  <p>Task root: <code>%s</code></p>
  <p>Useful links:</p>
  <ul>
    <li><a href="/agent/tasks/approvals?root=%s" target="_blank">Plan &amp; tool approvals</a></li>
    <li><a href="/agent/tasks/events?root=%s&view=1" target="_blank">Live event feed</a></li>
  </ul>
  <p>To start the demo from another terminal:</p>
  <pre><code>go run ./examples/demo/lds_report --mode run</code></pre>
  <p>Or via the HTTP API using <code>POST /agent/tasks/do</code> with <code>{"root":"%s","spec":{"goal":"..."}}</code>.</p>
</body>
</html>`, escapedRoot, rootParam, rootParam, template.HTMLEscapeString(root))
	})

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	log.Printf("LDS demo server listening on %s (root=%s)", addr, root)
	return httpServer.ListenAndServe()
}

func newDemoAgent(apiKey string, manager *sandbox.Manager) (*agentx.Agent, error) {
	provider := openai.New(
		openai.WithAPIKey(apiKey),
		openai.WithModel("gpt-5"),
	)

	httpFetch := tools.NewCoreAdapter(newHTTPFetchTool())
	sandboxExec := tools.NewSandboxCommand("sandbox_exec", "Execute shell inside sandbox", []string{"/bin/sh", "-lc", ""})

	pythonSkill, err := loadPythonSkill()
	if err != nil {
		return nil, err
	}

	return agentx.New(agentx.AgentOptions{
		ID:             "lds-demo-agent",
		Persona:        "You are a diligent research analyst. Cite credible sources and produce polished deliverables.",
		Provider:       provider,
		Tools:          []core.ToolHandle{httpFetch, sandboxExec},
		Skills:         []agentx.SkillBinding{{Name: "python", Skill: pythonSkill}},
		SandboxManager: manager,
		Approvals:      agentx.ApprovalPolicy{RequirePlan: true, RequireTools: []string{"sandbox_exec"}},
		DefaultBudgets: agentx.Budgets{
			MaxWallClock:            6 * time.Minute,
			MaxSteps:                12,
			MaxConsecutiveToolSteps: 3,
			MaxTokens:               12000,
		},
		RunnerOptions: []runner.RunnerOption{runner.WithToolTimeout(10 * time.Minute)},
	})
}

func loadPythonSkill() (*skills.Skill, error) {
	manifestPath := resolvePythonSkillPath()
	manifest, err := skills.LoadManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("load python skill manifest: %w", err)
	}
	pythonSkill, err := skills.New(manifest)
	if err != nil {
		return nil, fmt.Errorf("construct python skill: %w", err)
	}
	return pythonSkill, nil
}

func resolveTaskRoot(explicit string) (string, error) {
	root := strings.TrimSpace(explicit)
	if root == "" {
		root = strings.TrimSpace(os.Getenv("AGENT_TASK_ROOT"))
	}
	if root == "" {
		root = "examples/demo/lds_report"
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(abs, "progress"), 0o755); err != nil {
		return "", fmt.Errorf("ensure progress dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(abs, "progress", "plan"), 0o755); err != nil {
		return "", fmt.Errorf("ensure plan dir: %w", err)
	}
	return abs, nil
}

func newHTTPFetchTool() *tools.Tool[HTTPFetchInput, HTTPFetchOutput] {
	httpClient := &http.Client{Timeout: 20 * time.Second}
	return tools.New[HTTPFetchInput, HTTPFetchOutput](
		"http_fetch",
		"Fetch raw HTML/text from a URL",
		func(ctx context.Context, in HTTPFetchInput, meta core.ToolMeta) (HTTPFetchOutput, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, in.URL, nil)
			if err != nil {
				return HTTPFetchOutput{}, err
			}
			req.Header.Set("User-Agent", "gai-agent-demo/1.0")
			res, err := httpClient.Do(req)
			if err != nil {
				return HTTPFetchOutput{}, err
			}
			defer res.Body.Close()
			if res.StatusCode >= 400 {
				return HTTPFetchOutput{}, fmt.Errorf("http_fetch: status %d", res.StatusCode)
			}
			body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
			if err != nil {
				return HTTPFetchOutput{}, err
			}
			return HTTPFetchOutput{Status: res.StatusCode, Body: string(body)}, nil
		},
	)
}

func resolvePythonSkillPath() string {
	candidates := []string{
		os.Getenv("AGENT_PYTHON_SKILL"),
		"agents/python-sandbox/SKILLS/python/skill.yaml",
		"../agents/python-sandbox/SKILLS/python/skill.yaml",
		"../../agents/python-sandbox/SKILLS/python/skill.yaml",
		"../../../agents/python-sandbox/SKILLS/python/skill.yaml",
	}
	for _, path := range candidates {
		if strings.TrimSpace(path) == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	log.Fatal("unable to locate python sandbox skill; set AGENT_PYTHON_SKILL to the manifest path")
	return ""
}

type HTTPFetchInput struct {
	URL string `json:"url"`
}

type HTTPFetchOutput struct {
	Status int    `json:"status"`
	Body   string `json:"body"`
}
