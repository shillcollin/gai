# Agent Bundles

Agent bundles package one or more skills together with shared assets so they can be executed inside a sandboxed environment. Each bundle is a directory on disk that the SDK can load with `agent.LoadBundle`.

```
my-agent/
  agent.yaml                 # bundle manifest
  shared/                    # optional shared workspace mounted read-only
  SKILLS/
    python/
      skill.yaml             # skill-specific manifest
      workspace/             # files copied into the sandbox /workspace
      mounts/                # optional extra mounts (read-only by default)
      hooks/                 # optional scripts invoked during prewarm/postrun
      tests/                 # optional evaluation definitions
```

## agent.yaml

```yaml
name: python-sandbox-demo
version: "0.1.0"
default_skill: python
max_steps: 6
provider_preferences:
  - openai-responses
shared_workspace: shared
```

- `default_skill` is used when the caller does not specify a skill name.
- `max_steps` sets the default stop condition for the runner.
- `shared_workspace` (optional) points to a directory under the bundle root; it is mounted read-only at `/workspace/shared` inside the sandbox.

## Skill manifest (skill.yaml)

```yaml
name: python-sandbox
version: "1.0.0"
summary: Run short Python snippets inside an isolated container
instructions: |
  Use the `sandbox_exec` tool for every command. Prefer /bin/sh-compatible commands or
  short `python3 -c` snippets when creating files.
tools:
  - name: sandbox_exec
sandbox:
  warm: true
  workspace_dir: ./workspace
  prewarm:
    - name: install-python
      command: ["apk", "add", "--no-cache", "bash", "python3", "py3-pip"]
  session:
    runtime:
      image: alpine:3.19
      workdir: /workspace
    limits:
      timeout: 30s
      cpu_seconds: 20
  mounts:
    - source: ../shared
      target: /workspace/shared
      readonly: true
evaluations:
  - name: python-ready
    command: ["python3", "-c", "print('ready')"]
```

- `workspace_dir` is relative to the skill directory; it is copied verbatim into the sandbox before prewarm runs.
- `mounts` reference directories relative to the skill folder; they are mounted into the sandbox (read-only by default).
- `prewarm` commands are executed inside the sandbox before tool execution begins.

## Loading a bundle

```go
bundle, _ := agent.LoadBundle("agents/python-sandbox")
config, _ := bundle.SkillConfig("python")
assets := sandbox.SessionAssets{
    Workspace: config.Workspace,
    Mounts:    config.Mounts,
}
manager, _ := sandbox.NewManager(ctx, sandbox.ManagerOptions{})
r := runner.New(provider,
    runner.WithSkillAssets(config.Skill, manager, assets),
)
```

`bundle.SkillNames()` returns the available skills, and `bundle.SharedWorkspacePath()` exposes the shared directory if configured.

## CLI Support

The demo agent and CLI recognize bundles:

- `gai-cli agent inspect agents/python-sandbox`
- `gai-cli agent skills agents/python-sandbox`
- `go run ./examples/skills/sandbox_agent --bundle agents/python-sandbox`

Further tooling (initializers, evaluators) can be layered on top of this structure.
