# LDS Research Demo Agent

This demo shows how to build a governed AI worker with the GAI agent framework. The agent:

1. Reads the task brief in `task/brief.md`.
2. Researches the Church of Jesus Christ of Latter-day Saints (LDS) using the LLM and the `http_fetch` tool.
3. Plans the work, obtains an approval for the plan, and executes the steps.
4. Uses the Python sandbox skill to generate a PDF report.

## Prerequisites

- Go 1.21+
- Docker (required for the sandbox skill)
- OpenAI API key exported as `OPENAI_API_KEY`

## Run

```bash
export OPENAI_API_KEY=sk-...
go run ./examples/demo/lds_report
```

The agent writes all artifacts to `examples/demo/lds_report/progress/`. Look for the final PDF under `progress/artifacts/`.

## Customising

- To override the task root, set `AGENT_TASK_ROOT` to another directory containing `task/` and `progress/` folders.
- Adjust persona, tools, or budgets in `main.go` to fit other research tasks.
- Swap the provider via `agentx.AgentOptions.Provider`.

## Deliverables

- Findings and notes (`progress/findings.md`, `progress/notes.md`)
- Plan (`progress/plan/current.json`)
- Verification report (`progress/verify/0001-report.md`)
- Generated PDF (written to `progress/steps/act/` and registered in `DoTaskResult.Deliverables`)
