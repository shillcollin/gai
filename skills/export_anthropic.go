package skills

import (
	"encoding/json"
	"io"
	"strings"
)

// AnthropicDescriptor conforms to the SKILLS.md schema as closely as possible.
type AnthropicDescriptor struct {
	Name         string                 `json:"name"`
	Version      string                 `json:"version"`
	Summary      string                 `json:"summary"`
	Description  string                 `json:"description,omitempty"`
	Instructions string                 `json:"instructions"`
	Tags         []string               `json:"tags,omitempty"`
	Tools        []AnthropicTool        `json:"tools,omitempty"`
	Sandbox      AnthropicSandbox       `json:"sandbox"`
	Metadata     map[string]string      `json:"metadata,omitempty"`
	Extensions   map[string]interface{} `json:"extensions,omitempty"`
}

// AnthropicTool describes a tool exposed to the skill.
type AnthropicTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Optional    bool   `json:"optional,omitempty"`
}

// AnthropicSandbox captures runtime expectations for the skill.
type AnthropicSandbox struct {
	Image      string            `json:"image"`
	Workdir    string            `json:"workdir,omitempty"`
	Entrypoint []string          `json:"entrypoint,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	Warm       bool              `json:"warm"`
}

// AnthropicDescriptor builds the Anthropic descriptor representation.
func (s *Skill) AnthropicDescriptor() AnthropicDescriptor {
	rt := s.Manifest.Sandbox.Session.Runtime
	tools := make([]AnthropicTool, 0, len(s.Manifest.Tools))
	for _, tool := range s.Manifest.Tools {
		tools = append(tools, AnthropicTool{
			Name:        tool.Name,
			Description: tool.Description,
			Optional:    tool.Optional,
		})
	}
	return AnthropicDescriptor{
		Name:         s.Manifest.Name,
		Version:      s.Manifest.Version,
		Summary:      s.Manifest.Summary,
		Description:  s.Manifest.Description,
		Instructions: s.Manifest.Instructions,
		Tags:         append([]string(nil), s.Manifest.Tags...),
		Tools:        tools,
		Sandbox: AnthropicSandbox{
			Image:      rt.Image,
			Workdir:    rt.Workdir,
			Entrypoint: append([]string(nil), rt.Entrypoint...),
			Env:        cloneStringMap(rt.Env),
			Warm:       s.Manifest.Sandbox.Warm,
		},
		Metadata: s.Manifest.Metadata,
		Extensions: map[string]interface{}{
			"fingerprint": s.Fingerprint,
		},
	}
}

// WriteAnthropicDescriptor writes the descriptor to the writer in JSON format.
func (s *Skill) WriteAnthropicDescriptor(w io.Writer, indent bool) error {
	desc := s.AnthropicDescriptor()
	if indent {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(desc)
	}
	return json.NewEncoder(w).Encode(desc)
}

// Capabilities returns a synthesized capability string list used for documentation.
func (s *Skill) Capabilities() []string {
	capabilities := make([]string, 0, len(s.Manifest.Tools)+1)
	if s.Manifest.Sandbox.Warm {
		capabilities = append(capabilities, "sandbox:warm")
	} else {
		capabilities = append(capabilities, "sandbox:cold")
	}
	for _, tool := range s.Manifest.Tools {
		flag := "tool:" + strings.ToLower(tool.Name)
		capabilities = append(capabilities, flag)
	}
	return capabilities
}
