# Sandbox Skill Demo

A minimal web demo that showcases gai's skill + sandbox runtime. The agent loads a skill manifest, provisions a Dagger-backed container, and exposes an HTTP UI for prompting the model. Tool calls are executed inside the sandbox via `tools.NewSandboxCommand`.

## Prerequisites

1. Install the [Dagger CLI](https://docs.dagger.io/install) and ensure a container runtime (Docker, Colima, nerdctl) is running.
2. Copy `.env.example` (or create `.env`) in the repo root and add a provider API key, for example:
   ```env
   OPENAI_API_KEY=sk-...
   OPENAI_MODEL=gpt-4.1-mini # optional override
   ```
   The demo auto-detects providers in this order: OpenAI Chat, OpenAI Responses, Anthropic, Groq, xAI.
3. Ensure the sample agent bundle exists at `agents/python-sandbox/` (checked into the repo). It contains `agent.yaml`, a `SKILLS/python/` directory with the skill manifest, and starter workspace files copied into the sandbox at runtime.

## Run the demo

```bash
# from repo root
go run ./examples/skills/sandbox_agent --addr :8080 --bundle agents/python-sandbox
```

To run a specific skill from the bundle, add `--skill <name>` (the default is defined in `agent.yaml`).

Then open <http://localhost:8080>. Submit a prompt such as “Create a hello.py that prints the current time, run it, and show me the output.” The page will display the assistant response along with each sandbox tool invocation and result.

### Changing providers or models

- Use `--model` to force a specific model.
- Set `OPENAI_RESPONSES_API_KEY`, `ANTHROPIC_API_KEY`, `GROQ_API_KEY`, or `XAI_API_KEY` to switch providers. Optional env vars like `ANTHROPIC_MODEL` act as defaults if `--model` is omitted.

### Custom bundles

- Copy `agents/python-sandbox/` as a starting point or run the forthcoming `gai-cli agent init` (once implemented).
- Add new skills under `SKILLS/<skill-name>/` with their own `skill.yaml`, `workspace/`, `hooks/`, and `tests/` directories.
- Update `agent.yaml` to set the default skill, shared workspace, and max steps. Point the demo at your bundle with `--bundle path/to/agent`.

## How it works

1. The agent bundle defines default metadata (`agent.yaml`) and one or more skills under `SKILLS/`.
2. Each skill manifest describes instructions, sandbox runtime, workspace templates, mounts, and optional prewarm/evaluation commands.
3. `agent.LoadBundle` resolves workspace directories and mounts, and `sandbox.NewManager` prepares a container per request.
4. `runner.WithSkillAssets` wires the skill session (plus staged workspace) into the runner so tool metadata includes the live sandbox session.
5. The `sandbox_exec` tool runs shell commands in the container and streams results back through the normalized event feed.

Check `docs/SKILLS.md` for deeper guidance on authoring skills.
