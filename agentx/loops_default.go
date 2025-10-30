package agentx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	agentEvents "github.com/shillcollin/gai/agentx/events"
	"github.com/shillcollin/gai/agentx/plan"
	"github.com/shillcollin/gai/core"
)

// DefaultLoop implements the standard gather → plan → act → verify sequence.
var DefaultLoop = &Loop{
	Name:    "default",
	Version: "v1",
	Phases: []PhaseInfo{
		{Name: "gather", Description: "Gathering context and information", Weight: 0.2},
		{Name: "plan", Description: "Creating execution plan", Weight: 0.2},
		{Name: "act", Description: "Executing plan with tools", Weight: 0.4},
		{Name: "verify", Description: "Verifying results", Weight: 0.2},
	},
	Execute: executeDefaultLoop,
}

// SimpleActLoop is a minimal loop that just executes the act phase.
var SimpleActLoop = &Loop{
	Name:    "simple-act",
	Version: "v1",
	Phases: []PhaseInfo{
		{Name: "act", Description: "Acting on task", Weight: 1.0},
	},
	Execute: executeSimpleActLoop,
}

func executeDefaultLoop(lc *LoopContext) error {
	if err := lc.Phase("gather", func() error {
		return phaseGather(lc)
	}); err != nil {
		return err
	}

	if err := lc.Phase("plan", func() error {
		return phasePlan(lc)
	}); err != nil {
		return err
	}

	if err := lc.Phase("act", func() error {
		return phaseAct(lc)
	}); err != nil {
		return err
	}

	return lc.Phase("verify", func() error {
		return phaseVerify(lc)
	})
}

func executeSimpleActLoop(lc *LoopContext) error {
	return lc.Phase("act", func() error {
		return phaseAct(lc)
	})
}

// phaseGather assembles context from task inputs and produces findings.
func phaseGather(lc *LoopContext) error {
	summary, err := collectTaskContext(lc)
	if err != nil {
		return err
	}

	messages := buildPhaseMessages(lc, "gather", "Gather relevant information and record key findings as concise markdown bullet points.")
	messages = append(messages, core.UserMessage(core.TextPart(fmt.Sprintf(
		"Task goal: %s\n\nTask inputs:\n%s\n\nProduce findings.",
		lc.spec.Goal, summary,
	))))

	res, err := lc.Provider().GenerateText(lc.Ctx, core.Request{
		Messages:    messages,
		MaxTokens:   800,
		Temperature: 0.2,
	})
	if err != nil {
		return fmt.Errorf("gather generation failed: %w", err)
	}

	if err := lc.RecordUsage(res.Usage); err != nil {
		return err
	}
	lc.AddWarning(res.Warnings...)

	findingsPath := filepath.Join(lc.task.Progress("."), "findings.md")
	if err := writeAtomic(findingsPath, []byte(strings.TrimSpace(res.Text)+"\n")); err != nil {
		return err
	}

	lc.RecordStep(StepReport{
		ID:        "gather",
		Name:      "gather",
		Status:    "completed",
		OutputRef: relPath(lc.task.Progress("."), findingsPath),
		Usage:     res.Usage,
	})

	lc.SetOutputText(res.Text)
	return nil
}

// phasePlan prepares or validates the executable plan.
func phasePlan(lc *LoopContext) error {
	planDir := filepath.Join(lc.task.Progress("."), "plan")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		return fmt.Errorf("create plan dir: %w", err)
	}
	planPath := filepath.Join(planDir, "current.json")

	// Load existing plan or generate new one
	p, err := loadOrGeneratePlan(lc, planPath)
	if err != nil {
		return err
	}

	if len(p.AllowedTools) == 0 {
		p.AllowedTools = availableToolNames(lc)
	}
	if err := p.Validate(); err != nil {
		return err
	}

	sig, err := p.Signature()
	if err != nil {
		return err
	}

	lc.state.PlanSig = sig
	lc.state.UpdatedAt = agentEvents.Now()

	// Request plan approval if required
	if lc.agent.opts.Approvals.RequirePlan || lc.spec.RequirePlanApproval {
		if err := requestPlanApproval(lc, planPath, sig); err != nil {
			return err
		}
	}

	lc.RecordStep(StepReport{
		ID:        "plan",
		Name:      "plan",
		Status:    "completed",
		OutputRef: relPath(lc.task.Progress("."), planPath),
	})

	return nil
}

// phaseAct executes the plan using the runner and allowed tools.
func phaseAct(lc *LoopContext) error {
	// Load plan if not already in context
	planPath := filepath.Join(lc.task.Progress("."), "plan", "current.json")
	p, err := plan.LoadFromFile(planPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load plan: %w", err)
	}

	var planSteps, acceptance string
	if p != nil && len(p.Steps) > 0 {
		planSteps = strings.Join(p.Steps, "\n- ")
		acceptance = strings.Join(p.Acceptance, "\n- ")
	}

	toolHandles, err := prepareToolHandles(lc, p)
	if err != nil {
		return err
	}

	messages := buildPhaseMessages(lc, "act", "Execute the plan step-by-step, using tools when beneficial. Follow the plan strictly and provide a final response summarizing results.")
	if planSteps != "" {
		messages = append(messages,
			core.AssistantMessage(fmt.Sprintf("Plan:\n- %s", planSteps)),
			core.AssistantMessage(fmt.Sprintf("Acceptance criteria:\n- %s", acceptance)),
		)
	}
	messages = append(messages, core.UserMessage(core.TextPart(fmt.Sprintf("Task goal: %s", lc.spec.Goal))))

	req := core.Request{
		Messages:    messages,
		Tools:       toolHandles,
		ToolChoice:  core.ToolChoiceAuto,
		Temperature: 0.2,
	}

	var stops []core.StopCondition
	if lc.budgets.MaxSteps > 0 {
		stops = append(stops, core.MaxSteps(lc.budgets.MaxSteps))
	}
	if lc.budgets.MaxConsecutiveToolSteps > 0 {
		stops = append(stops, stopWhenMaxConsecutiveToolSteps(lc.budgets.MaxConsecutiveToolSteps))
	}
	stops = append(stops, core.NoMoreTools())
	req.StopWhen = core.Any(stops...)

	res, err := lc.Runner().ExecuteRequest(lc.Ctx, req)
	if err != nil {
		return fmt.Errorf("act execution failed: %w", err)
	}

	if err := lc.RecordUsage(res.Usage); err != nil {
		return err
	}
	lc.AddWarning(res.Warnings...)

	stepReports := convertRunnerSteps(res.Steps)
	for _, step := range stepReports {
		lc.RecordStep(step)
	}

	outputPath := filepath.Join(lc.task.Progress("."), "steps", "act", "output.md")
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create step dir: %w", err)
	}
	if err := writeAtomic(outputPath, []byte(res.Text)); err != nil {
		return err
	}

	lc.SetOutputText(res.Text)
	return nil
}

// phaseVerify validates acceptance criteria and records verification output.
func phaseVerify(lc *LoopContext) error {
	planPath := filepath.Join(lc.task.Progress("."), "plan", "current.json")
	p, _ := plan.LoadFromFile(planPath)

	acceptance := ""
	if p != nil && len(p.Acceptance) > 0 {
		acceptance = strings.Join(p.Acceptance, "\n- ")
	}

	messages := buildPhaseMessages(lc, "verify", "Verify whether the acceptance criteria have been met. Respond with a structured markdown report.")
	if acceptance != "" {
		messages = append(messages, core.AssistantMessage(fmt.Sprintf("Acceptance criteria:\n- %s", acceptance)))
	}
	messages = append(messages, core.UserMessage(core.TextPart(fmt.Sprintf("Final output to verify:\n%s", lc.outputText))))

	res, err := lc.Provider().GenerateText(lc.Ctx, core.Request{
		Messages:    messages,
		Temperature: 0.1,
		MaxTokens:   600,
	})
	if err != nil {
		return fmt.Errorf("verify generation failed: %w", err)
	}

	if err := lc.RecordUsage(res.Usage); err != nil {
		return err
	}
	lc.AddWarning(res.Warnings...)

	verifyDir := filepath.Join(lc.task.Progress("."), "verify")
	if err := os.MkdirAll(verifyDir, 0o755); err != nil {
		return fmt.Errorf("create verify dir: %w", err)
	}
	reportPath := filepath.Join(verifyDir, "0001-report.md")
	if err := writeAtomic(reportPath, []byte(res.Text)); err != nil {
		return err
	}

	lc.RecordStep(StepReport{
		ID:        "verify",
		Name:      "verify",
		Status:    "completed",
		OutputRef: relPath(lc.task.Progress("."), reportPath),
		Usage:     res.Usage,
	})

	return nil
}

// Helper functions

func collectTaskContext(lc *LoopContext) (string, error) {
	taskDir := lc.task.Task(".")
	var entries []string

	err := filepath.WalkDir(taskDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel := relPath(taskDir, path)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if len(data) > 32*1024 {
			data = data[:32*1024]
		}
		entries = append(entries, fmt.Sprintf("# %s\n%s", rel, string(data)))
		return nil
	})

	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("walk task inputs: %w", err)
	}

	sort.Strings(entries)
	return strings.Join(entries, "\n\n"), nil
}

func buildPhaseMessages(lc *LoopContext, phase, instruction string) []core.Message {
	persona := strings.TrimSpace(lc.agent.opts.Persona)
	if persona == "" {
		persona = "You are a helpful AI agent."
	}

	systemText := fmt.Sprintf("%s\nYou are working on phase: %s", persona, phase)
	messages := []core.Message{core.SystemMessage(systemText)}

	if instruction != "" {
		messages = append(messages, core.AssistantMessage(instruction))
	}

	// Add memory if available
	if lc.memoryMgr != nil {
		if pinned, err := lc.memoryMgr.BuildPinned(lc.Ctx, lc.task.Task(".")); err == nil {
			messages = append(messages, pinned...)
		}
		if phaseMem, err := lc.memoryMgr.BuildPhase(lc.Ctx, phase, nil); err == nil {
			messages = append(messages, phaseMem...)
		}
	}

	return messages
}

func loadOrGeneratePlan(lc *LoopContext, planPath string) (*plan.PlanV1, error) {
	// Try loading existing plan
	if p, err := plan.LoadFromFile(planPath); err == nil {
		return p, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read existing plan: %w", err)
	}

	// Generate new plan
	findings, _ := os.ReadFile(filepath.Join(lc.task.Progress("."), "findings.md"))
	toolNames := availableToolNames(lc)
	sort.Strings(toolNames)

	prompt := fmt.Sprintf(`You are planning the steps for an AI worker.
Goal: %s
Findings:
%s

Return a strict JSON object with fields:
- goal (string)
- steps (array of strings)
- allowed_tools (array of tool names selected from [%s])
- acceptance (array of strings)
`, lc.spec.Goal, string(findings), strings.Join(toolNames, ", "))

	messages := buildPhaseMessages(lc, "plan", "Generate a structured plan in JSON using the specified schema.")
	messages = append(messages, core.UserMessage(core.TextPart(prompt)))

	res, err := lc.Provider().GenerateText(lc.Ctx, core.Request{
		Messages:    messages,
		Temperature: 0,
		MaxTokens:   800,
	})
	if err != nil {
		return nil, fmt.Errorf("plan generation failed: %w", err)
	}

	if err := lc.RecordUsage(res.Usage); err != nil {
		return nil, err
	}
	lc.AddWarning(res.Warnings...)

	// Save raw response
	responsePath := filepath.Join(filepath.Dir(planPath), "latest_response.txt")
	if err := writeAtomic(responsePath, []byte(res.Text)); err != nil {
		return nil, fmt.Errorf("write plan response: %w", err)
	}

	// Extract and parse JSON
	raw := extractJSON(res.Text)
	rawPath := filepath.Join(filepath.Dir(planPath), "latest_raw.json")
	if err := writeAtomic(rawPath, []byte(strings.TrimSpace(raw)+"\n")); err != nil {
		return nil, fmt.Errorf("write plan raw: %w", err)
	}

	p, err := plan.ParseJSON([]byte(raw))
	if err != nil {
		return nil, fmt.Errorf("parse plan JSON: %w", err)
	}

	if len(p.AllowedTools) == 0 {
		p.AllowedTools = toolNames
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}

	// Save plan
	if err := plan.SaveToFile(planPath, p); err != nil {
		return nil, fmt.Errorf("save plan: %w", err)
	}

	return p, nil
}

func availableToolNames(lc *LoopContext) []string {
	names := make([]string, 0, len(lc.agent.toolIndex))
	for name := range lc.agent.toolIndex {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func stopWhenMaxConsecutiveToolSteps(limit int) core.StopCondition {
	if limit <= 0 {
		return func(state *core.RunnerState) (bool, core.StopReason) {
			return false, core.StopReason{}
		}
	}
	return func(state *core.RunnerState) (bool, core.StopReason) {
		if state == nil || len(state.Steps) == 0 {
			return false, core.StopReason{}
		}
		consecutive := 0
		for i := len(state.Steps) - 1; i >= 0; i-- {
			if len(state.Steps[i].ToolCalls) == 0 {
				break
			}
			consecutive++
		}
		if consecutive >= limit {
			return true, core.StopReason{Type: "budget.max_consecutive_tool_steps"}
		}
		return false, core.StopReason{}
	}
}

func extractJSON(text string) string {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimPrefix(trimmed, "```json")
		trimmed = strings.TrimPrefix(trimmed, "```JSON")
		trimmed = strings.TrimPrefix(trimmed, "```")
		if idx := strings.LastIndex(trimmed, "```"); idx >= 0 {
			trimmed = trimmed[:idx]
		}
		trimmed = strings.TrimSpace(trimmed)
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end >= start {
		return trimmed[start : end+1]
	}
	return trimmed
}

func convertRunnerSteps(steps []core.Step) []StepReport {
	reports := make([]StepReport, 0, len(steps))
	for _, s := range steps {
		report := StepReport{
			ID:        fmt.Sprintf("step-%d", s.Number),
			Name:      fmt.Sprintf("runner-step-%d", s.Number),
			Status:    "completed",
			StartedAt: s.StartedAt,
			EndedAt:   s.CompletedAt,
			ToolsUsed: extractToolNames(s.ToolCalls),
			ToolCalls: append([]core.ToolExecution(nil), s.ToolCalls...),
			Usage:     s.Usage,
		}
		reports = append(reports, report)
	}
	return reports
}

func extractToolNames(calls []core.ToolExecution) []string {
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		names = append(names, call.Call.Name)
	}
	sort.Strings(names)
	return names
}
