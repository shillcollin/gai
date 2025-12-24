// Package main demonstrates streaming text generation with the GAI v2 client.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/shillcollin/gai"
	"github.com/shillcollin/gai/core"
	_ "github.com/shillcollin/gai/providers/openai"
)

func main() {
	client := gai.NewClient()

	if !client.HasProvider("openai") {
		log.Fatal("OpenAI provider not available. Set OPENAI_API_KEY environment variable.")
	}

	ctx := context.Background()

	// Start streaming
	stream, err := client.Stream(ctx,
		gai.Request("openai/gpt-4o").
			System("You are a storyteller.").
			User("Tell me a short story about a robot learning to paint.").
			MaxTokens(300))
	if err != nil {
		log.Fatalf("Stream failed: %v", err)
	}
	defer stream.Close()

	fmt.Println("Streaming response:")

	// Process events as they arrive
	for event := range stream.Events() {
		switch event.Type {
		case core.EventTextDelta:
			// Print text as it streams in
			fmt.Print(event.TextDelta)

		case core.EventFinish:
			// Stream finished
			fmt.Printf("\n\n--- Stream Complete ---\n")
			fmt.Printf("Model: %s\n", event.Model)
			fmt.Printf("Tokens: %d total\n", event.Usage.TotalTokens)

		case core.EventError:
			log.Printf("Stream error: %v\n", event.Error)
		}
	}

	// Check for any stream-level errors
	if err := stream.Err(); err != nil && err != core.ErrStreamClosed {
		log.Fatalf("Stream error: %v", err)
	}

	// Alternative: collect all text at once
	fmt.Println("\n--- Using CollectText ---")

	stream2, err := client.Stream(ctx,
		gai.Request("openai/gpt-4o-mini").
			User("What are the three primary colors?"))
	if err != nil {
		log.Fatalf("Stream failed: %v", err)
	}

	text, err := stream2.CollectText()
	if err != nil {
		log.Fatalf("CollectText failed: %v", err)
	}
	fmt.Println(text)

	os.Exit(0)
}
