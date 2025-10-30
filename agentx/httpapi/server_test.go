package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shillcollin/gai/agentx"
	"github.com/shillcollin/gai/agentx/approvals"
	agentEvents "github.com/shillcollin/gai/agentx/events"
	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/runner"
	"github.com/shillcollin/gai/schema"
)

func TestDoTaskEndpoint(t *testing.T) {
	root := t.TempDir()
	taskDir := filepath.Join(root, "task")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "brief.md"), []byte("Test brief"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	server := NewServer(root, func() (*agentx.Agent, error) {
		provider := &stubProvider{}
		tool := &noopTool{name: "writer"}
		opts := agentx.AgentOptions{
			ID:        "agent-test",
			Persona:   "You are a test agent.",
			Provider:  provider,
			Tools:     []core.ToolHandle{tool},
			Approvals: agentx.ApprovalPolicy{},
			DefaultBudgets: agentx.Budgets{
				MaxWallClock: 10 * time.Second,
				MaxSteps:     5,
			},
			RunnerOptions: []runner.RunnerOption{runner.WithToolTimeout(5 * time.Second)},
		}
		opts.ApprovalBrokerFactory = approvalsStubFactory
		return agentx.New(opts)
	})

	payload := DoTaskRequest{Root: root, Spec: agentx.TaskSpec{Goal: "Test goal"}}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/agent/tasks/do", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.handleDoTask(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}

	var result agentx.DoTaskResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.TaskID == "" {
		t.Fatalf("expected task id in result")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/agent/tasks/"+result.TaskID+"/result", nil)
	getRec := httptest.NewRecorder()
	server.handleTaskResource(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("unexpected get status %d: %s", getRec.Code, getRec.Body.String())
	}

	ctx, cancel := context.WithCancel(context.Background())
	streamReq := httptest.NewRequest(http.MethodGet, "/agent/tasks/events?root="+root, nil).WithContext(ctx)
	writer := &streamRecorder{header: http.Header{}}
	done := make(chan struct{})
	go func() {
		server.handleStreamEvents(writer, streamReq)
		close(done)
	}()

	time.Sleep(150 * time.Millisecond)
	cancel()
	<-done
	if !strings.Contains(writer.buf.String(), agentEvents.SchemaVersionV1) {
		t.Fatalf("expected streamed events, got %s", writer.buf.String())
	}
}

func TestApprovalsEndpoints(t *testing.T) {
	root := t.TempDir()
	reqDir := filepath.Join(root, "progress", "approvals", "requests")
	if err := os.MkdirAll(reqDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	request := approvals.Request{
		ID:        "req123",
		Kind:      approvals.KindPlan,
		PlanPath:  "progress/plan/current.json",
		Rationale: "Plan approval test",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	data, _ := json.MarshalIndent(request, "", "  ")
	if err := os.WriteFile(filepath.Join(reqDir, request.ID+".json"), data, 0o644); err != nil {
		t.Fatalf("write request: %v", err)
	}

	srv := NewServer(root, func() (*agentx.Agent, error) { return nil, nil })
	mux := http.NewServeMux()
	srv.Register(mux)

	getReq := httptest.NewRequest(http.MethodGet, "/agent/tasks/approvals?root="+url.QueryEscape(root), nil)
	getReq.Header.Set("Accept", "text/html")
	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status %d", getRec.Code)
	}
	if !strings.Contains(getRec.Body.String(), request.ID) {
		t.Fatalf("expected request id in body")
	}

	form := url.Values{
		"root":        {root},
		"id":          {request.ID},
		"decision":    {string(approvals.StatusApproved)},
		"approved_by": {"tester"},
	}
	postReq := httptest.NewRequest(http.MethodPost, "/agent/tasks/approvals?root="+url.QueryEscape(root), strings.NewReader(form.Encode()))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRec := httptest.NewRecorder()
	mux.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusSeeOther {
		t.Fatalf("POST status %d", postRec.Code)
	}

	decisionPath := filepath.Join(root, "progress", "approvals", "decisions", request.ID+".json")
	if _, err := os.Stat(decisionPath); err != nil {
		t.Fatalf("decision file missing: %v", err)
	}
}

func TestEventsHTMLView(t *testing.T) {
	root := t.TempDir()
	progress := filepath.Join(root, "progress")
	if err := os.MkdirAll(progress, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(progress, "events.ndjson"), []byte("{\"type\":\"phase.start\"}\n"), 0o644); err != nil {
		t.Fatalf("write events: %v", err)
	}

	srv := NewServer(root, func() (*agentx.Agent, error) { return nil, nil })
	mux := http.NewServeMux()
	srv.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/agent/tasks/events?root="+url.QueryEscape(root), nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Agent Events") {
		t.Fatalf("expected html page")
	}
}

// approvalsStubFactory returns an auto-approving broker for tests.
func approvalsStubFactory(progressDir string) (approvals.Broker, error) {
	return &autoApproveBroker{}, nil
}

type autoApproveBroker struct{}

func (*autoApproveBroker) Submit(ctx context.Context, req approvals.Request) (string, error) {
	if req.ID == "" {
		req.ID = "req" + fmt.Sprint(time.Now().UnixNano())
	}
	return req.ID, nil
}

func (*autoApproveBroker) AwaitDecision(ctx context.Context, id string) (approvals.Decision, error) {
	return approvals.Decision{ID: id, Status: approvals.StatusApproved, Timestamp: time.Now().UTC()}, nil
}

func (*autoApproveBroker) GetDecision(ctx context.Context, id string) (approvals.Decision, bool, error) {
	return approvals.Decision{ID: id, Status: approvals.StatusApproved, Timestamp: time.Now().UTC()}, true, nil
}

type noopTool struct{ name string }

func (n *noopTool) Name() string                 { return n.name }
func (n *noopTool) Description() string          { return "noop tool" }
func (n *noopTool) InputSchema() *schema.Schema  { return nil }
func (n *noopTool) OutputSchema() *schema.Schema { return nil }
func (n *noopTool) Execute(context.Context, map[string]any, core.ToolMeta) (any, error) {
	return map[string]any{"ok": true}, nil
}

type streamRecorder struct {
	header http.Header
	buf    bytes.Buffer
}

func (s *streamRecorder) Header() http.Header         { return s.header }
func (s *streamRecorder) Write(b []byte) (int, error) { return s.buf.Write(b) }
func (s *streamRecorder) WriteHeader(statusCode int)  {}
func (s *streamRecorder) Flush()                      {}

type stubProvider struct {
	mu    sync.Mutex
	calls int
}

func (p *stubProvider) nextResult() core.TextResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	usage := core.Usage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30}
	switch p.calls {
	case 1:
		return core.TextResult{Text: "- Finding 1", Usage: usage}
	case 2:
		planJSON := `{"version":"plan.v1","goal":"Test","steps":["Run tool"],"allowed_tools":["writer"],"acceptance":["Tool executed"]}`
		return core.TextResult{Text: planJSON, Usage: usage}
	case 3:
		return core.TextResult{Text: "Execution complete", Usage: usage}
	default:
		return core.TextResult{Text: "All criteria satisfied", Usage: usage}
	}
}

func (p *stubProvider) GenerateText(ctx context.Context, req core.Request) (*core.TextResult, error) {
	res := p.nextResult()
	return &res, nil
}

func (p *stubProvider) StreamText(ctx context.Context, req core.Request) (*core.Stream, error) {
	res := p.nextResult()
	stream := core.NewStream(ctx, 0)
	go func() {
		defer stream.Close()
		stream.Push(core.StreamEvent{Type: core.EventTextDelta, TextDelta: res.Text, Schema: "gai.events.v1"})
		stream.Push(core.StreamEvent{Type: core.EventFinish, Schema: "gai.events.v1", FinishReason: &core.StopReason{Type: core.StopReasonComplete}, Usage: res.Usage})
	}()
	return stream, nil
}

func (p *stubProvider) GenerateObject(context.Context, core.Request) (*core.ObjectResultRaw, error) {
	return nil, errors.New("not supported")
}

func (p *stubProvider) StreamObject(context.Context, core.Request) (*core.ObjectStreamRaw, error) {
	return nil, errors.New("not supported")
}

func (p *stubProvider) Capabilities() core.Capabilities {
	return core.Capabilities{Provider: "stub"}
}
