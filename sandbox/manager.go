package sandbox

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Manager orchestrates sandbox sessions backed by a specific runtime backend.
type Manager struct {
	opts     ManagerOptions
	mu       sync.Mutex
	closed   bool
	sessions map[string]*Session
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewManager constructs a sandbox manager. When no backend is specified the Local backend is used.
func NewManager(ctx context.Context, opts ManagerOptions) (*Manager, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	mgrCtx, cancel := context.WithCancel(ctx)

	m := &Manager{
		opts:     opts,
		sessions: make(map[string]*Session),
		ctx:      mgrCtx,
		cancel:   cancel,
	}

	if m.opts.DefaultRuntime.Backend == "" {
		m.opts.DefaultRuntime.Backend = BackendLocal
	}
	return m, nil
}

// Close tears down the manager and closes all active sessions.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	for id, session := range m.sessions {
		_ = session.close()
		delete(m.sessions, id)
	}
	m.closed = true
	if m.cancel != nil {
		m.cancel()
	}
	return nil
}

// CreateSession provisions a fresh sandbox session.
func (m *Manager) CreateSession(ctx context.Context, spec SessionSpec) (*Session, error) {
	return m.CreateSessionWithAssets(ctx, spec, SessionAssets{})
}

// CreateSessionWithAssets provisions a sandbox session with additional host resources staged in.
func (m *Manager) CreateSessionWithAssets(ctx context.Context, spec SessionSpec, assets SessionAssets) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, fmt.Errorf("sandbox: manager closed")
	}
	session, err := m.newSession(ctx, spec, assets)
	if err != nil {
		return nil, err
	}
	m.sessions[session.ID] = session
	return session, nil
}

// Destroy tears down an existing session by identifier.
func (m *Manager) Destroy(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if session, ok := m.sessions[id]; ok {
		_ = session.close()
		delete(m.sessions, id)
	}
}

// Sessions returns a copy of currently tracked sessions.
func (m *Manager) Sessions() []*Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		out = append(out, session)
	}
	return out
}

func (m *Manager) newSession(ctx context.Context, spec SessionSpec, assets SessionAssets) (*Session, error) {
	rt := m.opts.DefaultRuntime
	rt = mergeRuntime(rt, spec.Runtime)
	spec.Runtime = rt

	if err := spec.Validate(); err != nil {
		return nil, err
	}

	// Dagger support removed. Only Local backend supported.
	if spec.Runtime.Backend != BackendLocal {
		return nil, ErrUnsupportedBackend
	}

	limits := m.opts.DefaultLimits.Merge(spec.Limits)

	session := &Session{
		ID:         generateSessionID(),
		manager:    m,
		runtime:    spec.Runtime,
		filesystem: spec.Filesystem,
		limits:     limits,
		metadata:   cloneStringMap(spec.Metadata),
	}
	return session, nil
}

func generateSessionID() string {
	return fmt.Sprintf("sandbox_%d", time.Now().UTC().UnixNano())
}

func mergeRuntime(base, override RuntimeSpec) RuntimeSpec {
	out := base
	if override.Backend != "" {
		out.Backend = override.Backend
	}
	if override.Workdir != "" {
		out.Workdir = override.Workdir
	}
	if len(override.Entrypoint) > 0 {
		out.Entrypoint = append([]string(nil), override.Entrypoint...)
	}
	if override.Env != nil {
		if out.Env == nil {
			out.Env = map[string]string{}
		}
		for k, v := range override.Env {
			out.Env[k] = v
		}
	}
	if override.Shell != "" {
		out.Shell = override.Shell
	}
	return out
}

// Session encapsulates a running sandbox environment.
type Session struct {
	ID         string
	manager    *Manager
	runtime    RuntimeSpec
	filesystem FilesystemSpec
	limits     ResourceLimits
	metadata   map[string]string

	mu     sync.Mutex
	closed bool
}

// Metadata returns a copy of the session metadata.
func (s *Session) Metadata() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneStringMap(s.metadata)
}

// Limits returns the base limits for the session.
func (s *Session) Limits() ResourceLimits {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.limits.Clone()
}

// Exec runs a command inside the sandbox.
func (s *Session) Exec(ctx context.Context, opts ExecOptions) (ExecResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ExecResult{}, ErrSessionClosed
	}
	if len(opts.Command) == 0 {
		return ExecResult{}, ErrCommandEmpty
	}
	return s.execLocked(ctx, opts)
}

func (s *Session) execLocked(ctx context.Context, opts ExecOptions) (ExecResult, error) {
	// Only Local backend supported
	return s.execLocal(ctx, opts)
}

func (s *Session) decorateCommand(command []string, limits ResourceLimits) []string {
	if len(command) == 0 {
		return command
	}
	var script strings.Builder
	if limits.CPUSeconds > 0 {
		script.WriteString("ulimit -t " + strconv.Itoa(limits.CPUSeconds) + ";")
	}
	if limits.MemoryBytes > 0 {
		kb := limits.MemoryBytes / 1024
		if kb < 1 {
			kb = 1
		}
		script.WriteString("ulimit -v " + strconv.FormatInt(kb, 10) + ";")
	}
	if limits.DiskBytes > 0 {
		blocks := limits.DiskBytes / 512
		if blocks < 1 {
			blocks = 1
		}
		script.WriteString("ulimit -f " + strconv.FormatInt(blocks, 10) + ";")
	}
	if script.Len() == 0 {
		return command
	}
	script.WriteString(strings.Join(quoteArgs(command), " "))
	shell := s.runtime.Shell
	if shell == "" {
		shell = "/bin/sh"
	}
	return []string{shell, "-lc", script.String()}
}

func quoteArgs(args []string) []string {
	out := make([]string, len(args))
	for i, arg := range args {
		if arg == "" {
			out[i] = "''"
			continue
		}
		if strings.ContainsAny(arg, " \t\"'`$&|<>") {
			out[i] = strconv.Quote(arg)
		} else {
			out[i] = arg
		}
	}
	return out
}

// Close terminates the session.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.close()
}

func (s *Session) close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return nil
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
