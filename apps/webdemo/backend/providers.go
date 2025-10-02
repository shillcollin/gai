package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/providers/anthropic"
	"github.com/shillcollin/gai/providers/gemini"
	"github.com/shillcollin/gai/providers/groq"
	openai "github.com/shillcollin/gai/providers/openai"
	openairesponses "github.com/shillcollin/gai/providers/openai-responses"
)

type providerEntry struct {
	Label        string
	DefaultModel string
	Models       []string
	Client       core.Provider
}

func buildProviders() (map[string]providerEntry, error) {
	providers := make(map[string]providerEntry)

	if key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); key != "" {
		chatClient := openai.New(
			openai.WithAPIKey(key),
			openai.WithModel("gpt-4.1-mini"),
		)
		providers["openai-chat"] = providerEntry{
			Label:        "OpenAI Chat",
			DefaultModel: "gpt-4.1-mini",
			Models:       []string{"gpt-4.1-mini", "gpt-4.1", "gpt-4o"},
			Client:       chatClient,
		}

		responsesClient := openairesponses.New(
			openairesponses.WithAPIKey(key),
			openairesponses.WithModel("o4-mini"),
		)
		providers["openai-responses"] = providerEntry{
			Label:        "OpenAI Responses",
			DefaultModel: "o4-mini",
			Models:       []string{"o4-mini", "o4", "gpt-4.1-mini", "gpt-5-codex"},
			Client:       responsesClient,
		}
	}

	if key := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")); key != "" {
		client := anthropic.New(
			anthropic.WithAPIKey(key),
			anthropic.WithModel("claude-3-7-sonnet-20250219"),
		)
		providers["anthropic"] = providerEntry{
			Label:        "Anthropic",
			DefaultModel: "claude-3-7-sonnet-20250219",
			Models:       []string{"claude-3-7-sonnet-20250219", "claude-3-5-haiku-20241022"},
			Client:       client,
		}
	}

	if key := strings.TrimSpace(os.Getenv("GOOGLE_API_KEY")); key != "" {
		client := gemini.New(
			gemini.WithAPIKey(key),
			gemini.WithModel("gemini-2.5-pro"),
		)
		providers["gemini"] = providerEntry{
			Label:        "Gemini",
			DefaultModel: "gemini-2.5-pro",
			Models:       []string{"gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.5-flash-lite"},
			Client:       client,
		}
	}

	if key := strings.TrimSpace(os.Getenv("GROQ_API_KEY")); key != "" {
		client := groq.New(
			groq.WithAPIKey(key),
			groq.WithModel("llama3-8b-8192"),
		)
		providers["groq"] = providerEntry{
			Label:        "Groq",
			DefaultModel: "llama3-8b-8192",
			Models:       []string{"llama3-8b-8192", "llama3-70b-8192", "mixtral-8x7b-32768", "gemma-7b-it"},
			Client:       client,
		}
	}

	if len(providers) == 0 {
		return nil, fmt.Errorf("no providers initialised; set API keys")
	}
	return providers, nil
}
