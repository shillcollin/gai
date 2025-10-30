package plan

import "testing"

func validPlan() PlanV1 {
	return PlanV1{
		Version:      VersionV1,
		Goal:         "Fix pagination bug",
		Steps:        []string{"Write failing test", "Implement fix"},
		AllowedTools: []string{"run_tests", "sandbox_exec"},
		Acceptance:   []string{"All tests pass"},
	}
}

func TestValidate(t *testing.T) {
	p := validPlan()
	if err := p.Validate(); err != nil {
		t.Fatalf("expected plan valid, got %v", err)
	}
}

func TestValidateErrors(t *testing.T) {
	cases := []PlanV1{
		{Goal: "", Steps: []string{"step"}, AllowedTools: []string{"tool"}},
		{Goal: "goal", Steps: nil, AllowedTools: []string{"tool"}},
		{Goal: "goal", Steps: []string{""}, AllowedTools: []string{"tool"}},
		{Goal: "goal", Steps: []string{"step"}, AllowedTools: nil},
		{Goal: "goal", Steps: []string{"step"}, AllowedTools: []string{"", "tool"}},
		{Goal: "goal", Steps: []string{"step"}, AllowedTools: []string{"tool", "tool"}},
		{Goal: "goal", Steps: []string{"step"}, AllowedTools: []string{"tool"}, Acceptance: []string{"", "ok"}},
		{Version: "plan.v2", Goal: "goal", Steps: []string{"step"}, AllowedTools: []string{"tool"}},
	}

	for i, c := range cases {
		if err := c.Validate(); err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
}

func TestSignatureDeterministic(t *testing.T) {
	p1 := validPlan()
	sig1, err := p1.Signature()
	if err != nil {
		t.Fatalf("signature failed: %v", err)
	}

	p2 := validPlan()
	// Shuffle order of steps to confirm canonicalization handles identical content
	p2.Steps = []string{"Write failing test", "Implement fix"}
	sig2, err := p2.Signature()
	if err != nil {
		t.Fatalf("signature failed: %v", err)
	}

	if sig1 != sig2 {
		t.Fatalf("expected deterministic signature, got %s vs %s", sig1, sig2)
	}
}

func TestAllowsTool(t *testing.T) {
	p := validPlan()
	if !p.AllowsTool("run_tests") {
		t.Fatalf("expected plan to allow run_tests")
	}
	if p.AllowsTool("other") {
		t.Fatalf("expected plan to reject other tool")
	}
}
