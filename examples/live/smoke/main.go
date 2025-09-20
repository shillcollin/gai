package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/providers/anthropic"
	"github.com/shillcollin/gai/providers/gemini"
	"github.com/shillcollin/gai/providers/openai"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tests := []struct {
		name string
		run  func(context.Context) error
	}{
		{name: "openai", run: runOpenAI},
		{name: "anthropic", run: runAnthropic},
		{name: "gemini", run: runGemini},
	}

	anyRan := false
	for _, test := range tests {
		if err := test.run(ctx); err != nil {
			if err == errSkipped {
				fmt.Printf("[%s] skipped (missing credentials)\n", test.name)
				continue
			}
			fmt.Printf("[%s] failed: %v\n", test.name, err)
			continue
		}
		anyRan = true
	}

	if !anyRan {
		fmt.Println("no live tests executed; check environment variables in .env")
		os.Exit(1)
	}
}

var errSkipped = fmt.Errorf("skipped")

func runOpenAI(ctx context.Context) error {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return errSkipped
	}
	client := openai.New(
		openai.WithAPIKey(key),
		openai.WithModel("gpt-4o-mini"),
	)
	res, err := client.GenerateText(ctx, core.Request{
		Messages:  []core.Message{core.UserMessage(core.TextPart("Say hello from gai"))},
		MaxTokens: 32,
	})
	if err != nil {
		return err
	}
	fmt.Printf("[openai] %s\n", res.Text)
	return nil
}

func runAnthropic(ctx context.Context) error {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return errSkipped
	}
	client := anthropic.New(
		anthropic.WithAPIKey(key),
		anthropic.WithModel("claude-3-haiku-20240307"),
	)
	res, err := client.GenerateText(ctx, core.Request{
		Messages:  []core.Message{core.UserMessage(core.TextPart("Give a two word greeting from gai"))},
		MaxTokens: 32,
	})
	if err != nil {
		return err
	}
	fmt.Printf("[anthropic] %s\n", res.Text)
	return nil
}

func runGemini(ctx context.Context) error {
	key := os.Getenv("GOOGLE_GENERATIVE_AI_API_KEY")
	if key == "" {
		return errSkipped
	}
	client := gemini.New(
		gemini.WithAPIKey(key),
		gemini.WithModel("gemini-1.5-flash-latest"),
	)
	res, err := client.GenerateText(ctx, core.Request{
		Messages:  []core.Message{core.UserMessage(core.TextPart("State the project name 'gai'"))},
		MaxTokens: 32,
	})
	if err != nil {
		return err
	}
	fmt.Printf("[gemini] %s\n", res.Text)
	return nil
}
