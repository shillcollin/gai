package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/shillcollin/gai/obs"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

func (s *Session) execLocal(ctx context.Context, opts ExecOptions) (ExecResult, error) {
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

	cmdStr := ""
	if len(opts.Command) > 0 {
		cmdStr = opts.Command[0]
		if len(opts.Command) > 1 {
			cmdStr += " " + fmt.Sprint(opts.Command[1:])
		}
	}

	attrs := []attribute.KeyValue{
		attribute.String("sandbox.session", s.ID),
		attribute.String("sandbox.backend", "local"),
		attribute.String("sandbox.command", cmdStr),
	}
	_, span := obs.Tracer().Start(execCtx, "sandbox.exec_local")
	span.SetAttributes(attrs...)
	defer span.End()

	// Apply templates locally
	for _, tmpl := range opts.Templates {
		path := tmpl.Path
		// If path is relative, join with current workdir (or Session workdir)
		// But spec says templates path must be absolute.
		// For local execution, writing absolute paths might be dangerous or permission denied.
		// However, "Claude Code" runs as the user.
		// If path is relative, exec.Command treats it relative to Dir.
		// But FileTemplate path is string.
		// Let's assume if it's relative we join with workdir.
		if !filepath.IsAbs(path) {
			wd := opts.Workdir
			if wd == "" {
				wd = s.runtime.Workdir
			}
			if wd == "" {
				// Fallback to OS Getwd
				osWd, _ := os.Getwd()
				wd = osWd
			}
			path = filepath.Join(wd, path)
		}

		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return ExecResult{}, fmt.Errorf("local: mkdir for template: %w", err)
		}
		perm := tmpl.Mode
		if perm == 0 {
			perm = 0644
		}
		if err := os.WriteFile(path, []byte(tmpl.Contents), perm); err != nil {
			return ExecResult{}, fmt.Errorf("local: write template: %w", err)
		}
	}

	// Construct command
	// We don't need "sh -c" unless user asks for it, but usually Command is [sh, -c, ...] or [ls, -la]
	// Dagger exec takes []string. os/exec takes (name, args...)
	if len(opts.Command) == 0 {
		return ExecResult{}, fmt.Errorf("empty command")
	}
	name := opts.Command[0]
	args := opts.Command[1:]

	cmd := exec.CommandContext(execCtx, name, args...)

	// Env
	cmd.Env = os.Environ() // Start with host env
	if s.runtime.Env != nil {
		for k, v := range s.runtime.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}
	for k, v := range opts.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Workdir
	if opts.Workdir != "" {
		cmd.Dir = opts.Workdir
	} else if s.runtime.Workdir != "" {
		cmd.Dir = s.runtime.Workdir
	}
	// If cmd.Dir is empty, os/exec uses current process dir, which is what we want.

	// Stdin
	if len(opts.Stdin) > 0 {
		cmd.Stdin = bytes.NewReader(opts.Stdin)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			// Command failed to start or other error
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			// Return error but also partial output?
			// Usually sandbox.Exec returns err only on system failure, not command failure.
			// But Run() returns error on non-zero exit too.
			// If it's ExitError, we handle it.
			// If it's "executable file not found", that's a system error from the perspective of "Exec".
			// But wait, `s.execLocked` returns error if ExitCode != 0?
			// No, `s.execLocked` checks `execContainer.ExitCode`. It returns nil error even if exitCode != 0.
			// So we should return nil error if it ran but failed.
			if _, ok := err.(*exec.Error); ok {
				// Failed to start
				return ExecResult{}, err
			}
			// Context deadline?
			if execCtx.Err() != nil {
				return ExecResult{}, execCtx.Err()
			}
		}
	}

	stdout := stdoutBuf.String()
	stderr := stderrBuf.String()

	if opts.Stdout != nil {
		opts.Stdout.Write(stdoutBuf.Bytes())
	}
	if opts.Stderr != nil {
		opts.Stderr.Write(stderrBuf.Bytes())
	}

	span.SetAttributes(
		attribute.Int("sandbox.exit_code", exitCode),
		attribute.Int64("sandbox.duration_ms", duration.Milliseconds()),
	)

	if exitCode != 0 {
		span.SetStatus(codes.Error, fmt.Sprintf("exit code %d", exitCode))
	} else {
		span.SetStatus(codes.Ok, "")
	}

	return ExecResult{
		Command:       opts.Command,
		ExitCode:      exitCode,
		Duration:      duration,
		Stdout:        stdout,
		Stderr:        stderr,
		AppliedLimits: limits,
	}, nil
}
