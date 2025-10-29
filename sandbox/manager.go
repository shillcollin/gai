package sandbox

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"dagger.io/dagger"
	"github.com/shillcollin/gai/obs"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// Manager orchestrates sandbox sessions backed by a specific runtime backend.
type Manager struct {
	opts     ManagerOptions
	mu       sync.Mutex
	client   daggerClient
	closed   bool
	sessions map[string]*Session
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewManager constructs a sandbox manager. When no backend is specified the Dagger backend is used.
func NewManager(ctx context.Context, opts ManagerOptions) (*Manager, error) {
	// The Dagger client ties the engine lifecycle to the Connect context.
	// Use a long-lived context we can cancel on Manager.Close.
	if ctx == nil {
		ctx = context.Background()
	}
	mgrCtx, cancel := context.WithCancel(ctx)

	factory := opts.clientFactory
	if factory == nil {
		factory = defaultDaggerFactory{}
	}

	client, err := factory.Connect(mgrCtx, opts.DaggerOpts...)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("sandbox: connect dagger: %w", err)
	}

	m := &Manager{
		opts:     opts,
		client:   client,
		sessions: make(map[string]*Session),
		ctx:      mgrCtx,
		cancel:   cancel,
	}

	if m.opts.DefaultRuntime.Image == "" {
		m.opts.DefaultRuntime.Image = "ghcr.io/catthehacker/ubuntu:jammy"
	}
	if m.opts.DefaultRuntime.Shell == "" {
		m.opts.DefaultRuntime.Shell = "/bin/sh"
	}
	if m.opts.DefaultRuntime.Backend == "" {
		m.opts.DefaultRuntime.Backend = BackendDagger
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
	if m.client != nil {
		return m.client.Close()
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
	if spec.Runtime.Backend != BackendDagger {
		return nil, ErrUnsupportedBackend
	}

	container := m.client.Container()
	if spec.Runtime.Platform != "" {
		container = m.client.Container(dagger.ContainerOpts{Platform: dagger.Platform(spec.Runtime.Platform)})
	}
	container = container.From(spec.Runtime.Image)
	for key, value := range spec.Runtime.Env {
		container = container.WithEnvVariable(key, value)
	}
	if spec.Runtime.Workdir != "" {
		container = container.WithWorkdir(spec.Runtime.Workdir)
	}
	if len(spec.Runtime.Entrypoint) > 0 {
		container = container.WithEntrypoint(spec.Runtime.Entrypoint)
	}

	var err error
	container, err = m.applyFilesystem(container, spec.Filesystem, spec.Runtime, assets)
	if err != nil {
		return nil, err
	}

	limits := m.opts.DefaultLimits.Merge(spec.Limits)

	session := &Session{
		ID:         generateSessionID(),
		manager:    m,
		client:     m.client,
		container:  container,
		runtime:    spec.Runtime,
		filesystem: spec.Filesystem,
		limits:     limits,
		metadata:   cloneStringMap(spec.Metadata),
	}
	return session, nil
}

func (m *Manager) applyFilesystem(container *dagger.Container, fsSpec FilesystemSpec, runtime RuntimeSpec, assets SessionAssets) (*dagger.Container, error) {
	result := container
	for _, tmpl := range fsSpec.Templates {
		mode := 0
		if tmpl.Mode != 0 {
			mode = int(tmpl.Mode.Perm())
		}
		opts := dagger.ContainerWithNewFileOpts{
			Permissions: mode,
		}
		if tmpl.Owner != "" {
			opts.Owner = tmpl.Owner
		}
		result = result.WithNewFile(tmpl.Path, tmpl.Contents, opts)
	}

	for _, mount := range fsSpec.Mounts {
		if err := mount.Validate(); err != nil {
			return nil, err
		}
		hostDir := m.client.Host().Directory(mount.Source)
		if mount.ReadOnly {
			// Copy contents into container to keep read-only semantics.
			result = result.WithDirectory(mount.Target, hostDir)
		} else {
			result = result.WithMountedDirectory(mount.Target, hostDir)
		}
	}

	if assets.Workspace != "" {
		hostDir := m.client.Host().Directory(assets.Workspace)
		target := runtime.Workdir
		if target == "" {
			target = "/workspace"
		}
		result = result.WithDirectory(target, hostDir)
	}
	for _, mount := range assets.Mounts {
		hostDir := m.client.Host().Directory(mount.Source)
		if mount.ReadOnly {
			result = result.WithDirectory(mount.Target, hostDir)
		} else {
			result = result.WithMountedDirectory(mount.Target, hostDir)
		}
	}
	return result, nil
}

func generateSessionID() string {
	return fmt.Sprintf("sandbox_%d", time.Now().UTC().UnixNano())
}

func mergeRuntime(base, override RuntimeSpec) RuntimeSpec {
	out := base
	if override.Backend != "" {
		out.Backend = override.Backend
	}
	if override.Image != "" {
		out.Image = override.Image
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
	if override.Platform != "" {
		out.Platform = override.Platform
	}
	return out
}

// Session encapsulates a running sandbox environment.
type Session struct {
	ID         string
	manager    *Manager
	client     daggerClient
	container  *dagger.Container
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
	limits := s.limits.Clone()
	if opts.Timeout > 0 {
		limits.Timeout = opts.Timeout
	}

	execCtx := ctx
	if limits.Timeout > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, limits.Timeout)
		defer cancel()
	}

	attrs := []attribute.KeyValue{
		attribute.String("sandbox.session", s.ID),
		attribute.String("sandbox.image", s.runtime.Image),
		attribute.StringSlice("sandbox.command", opts.Command),
	}
	spanCtx, span := obs.Tracer().Start(execCtx, "sandbox.exec")
	span.SetAttributes(attrs...)
	defer span.End()

	container := s.container
	if opts.Workdir != "" {
		container = container.WithWorkdir(opts.Workdir)
	}
	for key, value := range opts.Env {
		container = container.WithEnvVariable(key, value)
	}

	var err error
	container, err = s.applyEphemeralFilesystem(container, opts)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return ExecResult{}, err
	}

	execArgs := s.decorateCommand(opts.Command, limits)
	execOpts := []dagger.ContainerWithExecOpts{
		{Expect: dagger.ReturnTypeAny},
	}
	if len(opts.Stdin) > 0 {
		execOpts = append(execOpts, dagger.ContainerWithExecOpts{Stdin: string(opts.Stdin)})
	}
	execContainer := container.WithExec(execArgs, execOpts...)

	start := time.Now()
	stdout, err := execContainer.Stdout(spanCtx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return ExecResult{}, err
	}
	stderr, err := execContainer.Stderr(spanCtx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return ExecResult{}, err
	}
	exitCode, err := execContainer.ExitCode(spanCtx)
	duration := time.Since(start)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return ExecResult{}, err
	}

	if opts.Stdout != nil && stdout != "" {
		_, _ = opts.Stdout.Write([]byte(stdout))
	}
	if opts.Stderr != nil && stderr != "" {
		_, _ = opts.Stderr.Write([]byte(stderr))
	}

	s.container = execContainer

	if exitCode != 0 {
		span.SetStatus(codes.Error, fmt.Sprintf("exit code %d", exitCode))
	} else {
		span.SetStatus(codes.Ok, "")
	}
	span.SetAttributes(
		attribute.Int("sandbox.exit_code", exitCode),
		attribute.Int64("sandbox.duration_ms", duration.Milliseconds()),
	)

	return ExecResult{
		Command:       append([]string(nil), opts.Command...),
		ExitCode:      exitCode,
		Duration:      duration,
		Stdout:        stdout,
		Stderr:        stderr,
		AppliedLimits: limits,
	}, nil
}

func (s *Session) applyEphemeralFilesystem(container *dagger.Container, opts ExecOptions) (*dagger.Container, error) {
	result := container
	for _, tmpl := range opts.Templates {
		if err := tmpl.Validate(); err != nil {
			return nil, err
		}
		mode := 0
		if tmpl.Mode != 0 {
			mode = int(tmpl.Mode.Perm())
		}
		opt := dagger.ContainerWithNewFileOpts{Permissions: mode}
		if tmpl.Owner != "" {
			opt.Owner = tmpl.Owner
		}
		result = result.WithNewFile(tmpl.Path, tmpl.Contents, opt)
	}
	for _, mount := range opts.Mounts {
		if err := mount.Validate(); err != nil {
			return nil, err
		}
		dir := s.client.Host().Directory(mount.Source)
		if mount.ReadOnly {
			result = result.WithDirectory(mount.Target, dir)
		} else {
			result = result.WithMountedDirectory(mount.Target, dir)
		}
	}
	return result, nil
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
