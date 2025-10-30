package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shillcollin/gai/agentx"
	"github.com/shillcollin/gai/agentx/approvals"
	"github.com/shillcollin/gai/agentx/internal/id"
)

// Server exposes HTTP endpoints for running and inspecting agent tasks.
type Server struct {
	BaseDir string

	agentFactory func() (*agentx.Agent, error)

	mu      sync.RWMutex
	results map[string]*taskRecord
}

type taskRecord struct {
	Result agentx.DoTaskResult
	Err    error
	Done   bool
}

// NewServer constructs a new Server using the provided agent factory.
func NewServer(baseDir string, factory func() (*agentx.Agent, error)) *Server {
	return &Server{
		BaseDir:      baseDir,
		agentFactory: factory,
		results:      make(map[string]*taskRecord),
	}
}

// Register wires the server endpoints onto the provided mux.
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("/agent/tasks/do", s.handleDoTask)
	mux.HandleFunc("/agent/tasks/do/async", s.handleDoTaskAsync)
	mux.HandleFunc("/agent/tasks/events", s.handleStreamEvents)
	mux.HandleFunc("/agent/tasks/", s.handleTaskResource)
	mux.HandleFunc("/agent/tasks/approvals", s.handleApprovals)
}

type DoTaskRequest struct {
	Root    string            `json:"root"`
	Spec    agentx.TaskSpec   `json:"spec"`
	Options DoTaskRequestOpts `json:"options,omitempty"`
}

type DoTaskRequestOpts struct {
	ResultView string `json:"result_view,omitempty"`
}

type asyncResponse struct {
	JobID  string `json:"job_id"`
	TaskID string `json:"task_id"`
}

func (s *Server) handleDoTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload DoTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, fmt.Sprintf("invalid body: %v", err), http.StatusBadRequest)
		return
	}

	agent, err := s.agentFactory()
	if err != nil {
		http.Error(w, fmt.Sprintf("agent init failed: %v", err), http.StatusInternalServerError)
		return
	}

	root, err := s.resolveRoot(payload.Root)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	opts := []agentx.DoTaskOption{toResultViewOption(payload.Options.ResultView)}
	result, err := agent.DoTask(r.Context(), root, payload.Spec, opts...)
	if err != nil {
		http.Error(w, fmt.Sprintf("task failed: %v", err), http.StatusInternalServerError)
		s.storeResult(result.TaskID, result, err)
		return
	}
	s.storeResult(result.TaskID, result, nil)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleDoTaskAsync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload DoTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, fmt.Sprintf("invalid body: %v", err), http.StatusBadRequest)
		return
	}

	agent, err := s.agentFactory()
	if err != nil {
		http.Error(w, fmt.Sprintf("agent init failed: %v", err), http.StatusInternalServerError)
		return
	}

	root, err := s.resolveRoot(payload.Root)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	jobID, err := id.New()
	if err != nil {
		http.Error(w, fmt.Sprintf("generate job id failed: %v", err), http.StatusInternalServerError)
		return
	}

	opts := []agentx.DoTaskOption{toResultViewOption(payload.Options.ResultView)}

	go func() {
		result, err := agent.DoTask(context.Background(), root, payload.Spec, opts...)
		s.storeResult(jobID, result, err)
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(asyncResponse{JobID: jobID})
}

func (s *Server) handleTaskResource(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/agent/tasks/") {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/agent/tasks/"), "/")
	if len(parts) == 0 {
		http.NotFound(w, r)
		return
	}
	id := parts[0]
	if len(parts) == 1 || parts[1] == "result" {
		s.handleGetResult(w, r, id)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handleGetResult(w http.ResponseWriter, r *http.Request, id string) {
	s.mu.RLock()
	rec, ok := s.results[id]
	s.mu.RUnlock()
	if !ok {
		http.Error(w, "unknown task", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rec.Result)
}

func (s *Server) handleStreamEvents(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	if root == "" {
		http.Error(w, "root query parameter required", http.StatusBadRequest)
		return
	}
	abs, err := s.resolveRoot(root)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.Contains(r.Header.Get("Accept"), "text/html") || r.URL.Query().Get("view") == "1" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := eventsTemplate.Execute(w, root); err != nil {
			http.Error(w, fmt.Sprintf("render events: %v", err), http.StatusInternalServerError)
		}
		return
	}

	eventsPath := filepath.Join(abs, "progress", "events.ndjson")

	file, err := os.Open(eventsPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("open events: %v", err), http.StatusInternalServerError)
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", "application/x-ndjson")
	flusher, _ := w.(http.Flusher)
	reader := bufio.NewReader(file)

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if _, werr := w.Write(line); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if errors.Is(err, io.EOF) {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(200 * time.Millisecond):
				continue
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *Server) resolveRoot(root string) (string, error) {
	if root == "" {
		return "", errors.New("root is required")
	}
	cleaned := filepath.Clean(root)
	if !filepath.IsAbs(cleaned) {
		cleaned = filepath.Join(s.BaseDir, cleaned)
	}
	if s.BaseDir != "" {
		base := filepath.Clean(s.BaseDir)
		rel, err := filepath.Rel(base, cleaned)
		if err != nil || strings.HasPrefix(rel, "..") {
			return "", errors.New("root outside of base directory")
		}
	}
	return cleaned, nil
}

func (s *Server) storeResult(id string, result agentx.DoTaskResult, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results[id] = &taskRecord{Result: result, Err: err, Done: true}
}

func (s *Server) handleApprovals(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	abspath, err := s.resolveRoot(root)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	requests, err := listApprovalRequests(abspath)
	if err != nil {
		http.Error(w, fmt.Sprintf("load approvals: %v", err), http.StatusInternalServerError)
		return
	}

	switch r.Method {
	case http.MethodGet:
		accept := r.Header.Get("Accept")
		if strings.Contains(accept, "text/html") || accept == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if err := approvalsTemplate.Execute(w, approvalsPageData{Root: root, Requests: requests}); err != nil {
				http.Error(w, fmt.Sprintf("render: %v", err), http.StatusInternalServerError)
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(requests)
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, fmt.Sprintf("parse form: %v", err), http.StatusBadRequest)
			return
		}
		id := r.FormValue("id")
		decision := r.FormValue("decision")
		approvedBy := r.FormValue("approved_by")
		reason := r.FormValue("reason")
		if id == "" || !isValidDecision(decision) {
			http.Error(w, "invalid approval submission", http.StatusBadRequest)
			return
		}
		rec := approvals.Decision{
			ID:         id,
			Status:     approvals.Status(decision),
			ApprovedBy: approvedBy,
			Timestamp:  time.Now().UTC(),
			Reason:     reason,
		}
		if err := writeApprovalDecision(abspath, rec); err != nil {
			http.Error(w, fmt.Sprintf("write decision: %v", err), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/agent/tasks/approvals?root=%s", url.QueryEscape(root)), http.StatusSeeOther)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func listApprovalRequests(root string) ([]approvalEntry, error) {
	dir := filepath.Join(root, "progress", "approvals", "requests")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []approvalEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var req approvals.Request
		if err := json.Unmarshal(data, &req); err != nil {
			return nil, err
		}
		if req.ID == "" {
			req.ID = strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		}
		out = append(out, approvalEntry{Request: req, Filename: e.Name()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Request.ExpiresAt.After(out[j].Request.ExpiresAt) })
	return out, nil
}

func writeApprovalDecision(root string, decision approvals.Decision) error {
	if decision.ID == "" {
		return errors.New("decision id required")
	}
	if decision.Status == "" {
		return errors.New("decision status required")
	}
	dir := filepath.Join(root, "progress", "approvals", "decisions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, decision.ID+".json")
	data, err := json.MarshalIndent(decision, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func isValidDecision(status string) bool {
	switch approvals.Status(strings.ToLower(status)) {
	case approvals.StatusApproved, approvals.StatusDenied, approvals.StatusExpired:
		return true
	default:
		return false
	}
}

type approvalEntry struct {
	Request  approvals.Request
	Filename string
}

type approvalsPageData struct {
	Root     string
	Requests []approvalEntry
}

var approvalsTemplate = template.Must(template.New("approvals").Funcs(template.FuncMap{
	"escape": template.HTMLEscapeString,
}).Parse(`<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>Agent Approvals</title>
  <style>
    body { font-family: sans-serif; margin: 2rem; }
    table { border-collapse: collapse; width: 100%; margin-bottom: 2rem; }
    th, td { border: 1px solid #ccc; padding: 0.5rem; text-align: left; }
    form { display: inline; }
    textarea { width: 100%; height: 4rem; }
  </style>
</head>
<body>
  <h1>Pending Approvals</h1>
  {{if .Requests}}
  <table>
    <thead>
      <tr><th>ID</th><th>Type</th><th>Details</th><th>Actions</th></tr>
    </thead>
    <tbody>
    {{range .Requests}}
      <tr>
        <td>{{.Request.ID}}</td>
        <td>{{.Request.Kind}}</td>
        <td>
          {{if .Request.PlanPath}}Plan Path: {{.Request.PlanPath}}<br>{{end}}
          {{if .Request.ToolName}}Tool: {{.Request.ToolName}}<br>{{end}}
          {{if .Request.Rationale}}Reason: {{.Request.Rationale}}<br>{{end}}
          Expires: {{.Request.ExpiresAt}}
        </td>
        <td>
          <form method="post">
            <input type="hidden" name="root" value="{{$.Root}}">
            <input type="hidden" name="id" value="{{.Request.ID}}">
            <input type="hidden" name="decision" value="approved">
            <input type="text" name="approved_by" placeholder="Approved by">
            <textarea name="reason" placeholder="Notes (optional)"></textarea>
            <button type="submit">Approve</button>
          </form>
          <form method="post">
            <input type="hidden" name="root" value="{{$.Root}}">
            <input type="hidden" name="id" value="{{.Request.ID}}">
            <input type="hidden" name="decision" value="denied">
            <input type="text" name="approved_by" placeholder="Reviewed by">
            <textarea name="reason" placeholder="Reason for denial"></textarea>
            <button type="submit">Deny</button>
          </form>
        </td>
      </tr>
    {{end}}
    </tbody>
  </table>
  {{else}}
  <p>No pending approvals for root <code>{{.Root}}</code>.</p>
  {{end}}
</body>
</html>`))

var eventsTemplate = template.Must(template.New("events").Parse(`<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>Agent Events</title>
  <style>
    body { font-family: sans-serif; margin: 2rem; }
    pre { background: #f4f4f4; padding: 1rem; border-radius: 4px; max-height: 70vh; overflow: auto; }
    .controls { margin-bottom: 1rem; }
  </style>
</head>
<body>
  <h1>Agent Events</h1>
  <div class="controls">
    <label>Root: <code>{{.}}</code></label>
  </div>
  <pre id="log"></pre>

  <script>
    const root = encodeURIComponent({{printf "%q" .}});
    const eventsUrl = "/agent/tasks/events?root=" + root;
    const logEl = document.getElementById('log');
    let lastLength = 0;

    async function load() {
      try {
        const resp = await fetch(eventsUrl, { headers: { 'Accept': 'application/x-ndjson' }});
        const text = await resp.text();
        if (text.length !== lastLength) {
          logEl.textContent = text.trim();
          logEl.scrollTop = logEl.scrollHeight;
          lastLength = text.length;
        }
      } catch (err) {
        console.error('load events', err);
      }
    }

    load();
    setInterval(load, 2000);
  </script>
</body>
</html>`))

func toResultViewOption(value string) agentx.DoTaskOption {
	switch strings.ToLower(value) {
	case "summary":
		return agentx.WithResultView(agentx.ResultViewSummary)
	case "full":
		return agentx.WithResultView(agentx.ResultViewFull)
	default:
		return agentx.WithResultView(agentx.ResultViewMinimal)
	}
}
