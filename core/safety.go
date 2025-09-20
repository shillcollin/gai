package core

// SafetyConfig configures provider-specific safety thresholds.
type SafetyConfig struct {
	Harassment SafetyLevel `json:"harassment,omitempty"`
	Hate       SafetyLevel `json:"hate,omitempty"`
	Sexual     SafetyLevel `json:"sexual,omitempty"`
	Dangerous  SafetyLevel `json:"dangerous,omitempty"`
	SelfHarm   SafetyLevel `json:"self_harm,omitempty"`
	Other      SafetyLevel `json:"other,omitempty"`
}

// SafetyLevel enumerates filtering thresholds.
type SafetyLevel string

const (
	SafetyNone   SafetyLevel = "none"
	SafetyLow    SafetyLevel = "low"
	SafetyMedium SafetyLevel = "medium"
	SafetyHigh   SafetyLevel = "high"
	SafetyBlock  SafetyLevel = "block"
)

// SafetyEvent records provider safety triggers during streaming.
type SafetyEvent struct {
	Category string  `json:"category"`
	Action   string  `json:"action"`
	Score    float32 `json:"score"`
	Target   string  `json:"target"`
	Start    int     `json:"start,omitempty"`
	End      int     `json:"end,omitempty"`
}
