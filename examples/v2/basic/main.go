// Package main demonstrates basic text generation with the GAI v2 client.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/shillcollin/gai"
	_ "github.com/shillcollin/gai/providers/openai" // Register OpenAI provider
)

func main() {
	// Create a new client - providers are auto-configured from environment variables
	client := gai.NewClient()

	// Verify OpenAI provider is available
	if !client.HasProvider("openai") {
		log.Fatal("OpenAI provider not available. Set OPENAI_API_KEY environment variable.")
	}

	ctx := context.Background()

	// Simple text generation
	text, err := client.Text(ctx,
		gai.Request("openai/gpt-4o").
			System("You are a helpful assistant.").
			User("What is the capital of France?"))
	if err != nil {
		log.Fatalf("Text generation failed: %v", err)
	}

	fmt.Println("Response:", text)

	// Full result with metadata
	result, err := client.Generate(ctx,
		gai.Request("openai/gpt-4o").
			System("You are a concise assistant.").
			User("List 3 facts about Go programming language.").
			Temperature(0.7).
			MaxTokens(200))
	if err != nil {
		log.Fatalf("Generate failed: %v", err)
	}

	fmt.Println("\n--- Full Result ---")
	fmt.Println("Text:", result.Text())
	fmt.Println("Model:", result.Model())
	fmt.Println("Provider:", result.Provider())
	fmt.Printf("Tokens: %d input, %d output, %d total\n",
		result.InputTokens(), result.OutputTokens(), result.TotalTokens())
	fmt.Printf("Latency: %dms\n", result.LatencyMS())

	// Using aliases
	clientWithAlias := gai.NewClient(
		gai.WithAlias("fast", "openai/gpt-4o-mini"),
		gai.WithAlias("smart", "openai/gpt-4o"),
	)

	text, err = clientWithAlias.Text(ctx,
		gai.Request("fast").User("Say hello in 5 words or less"))
	if err != nil {
		log.Fatalf("Alias request failed: %v", err)
	}
	fmt.Println("\nFast model response:", text)

	os.Exit(0)
}
