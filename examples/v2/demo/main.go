// Package main is a live demo of the GAI v2 client API.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/shillcollin/gai"
	"github.com/shillcollin/gai/core"
	_ "github.com/shillcollin/gai/providers/openai"
)

func main() {
	client := gai.NewClient()

	if !client.HasProvider("openai") {
		log.Fatal("OpenAI provider not available")
	}

	ctx := context.Background()

	fmt.Println("=== GAI v2 Live Demo ===")
	fmt.Println()

	// 1. Simple text generation
	fmt.Println("1. Simple Text Generation")
	fmt.Println("--------------------------")
	text, err := client.Text(ctx,
		gai.Request("openai/gpt-4o-mini").
			User("What is 2+2? Answer in one word."))
	if err != nil {
		log.Fatalf("Text failed: %v", err)
	}
	fmt.Printf("Response: %s\n\n", text)

	// 2. With system prompt and parameters
	fmt.Println("2. With System Prompt")
	fmt.Println("----------------------")
	result, err := client.Generate(ctx,
		gai.Request("openai/gpt-4o-mini").
			System("You are a pirate. Respond in pirate speak.").
			User("Tell me about the weather.").
			Temperature(0.9).
			MaxTokens(100))
	if err != nil {
		log.Fatalf("Generate failed: %v", err)
	}
	fmt.Printf("Response: %s\n", result.Text())
	fmt.Printf("Tokens: %d input, %d output\n\n", result.InputTokens(), result.OutputTokens())

	// 3. Structured output
	fmt.Println("3. Structured Output")
	fmt.Println("--------------------")
	var info struct {
		Capital    string `json:"capital"`
		Population int64  `json:"population"`
		Language   string `json:"language"`
	}
	err = client.Unmarshal(ctx,
		gai.Request("openai/gpt-4o-mini").
			User("Give me basic facts about France: capital, population (as number), and main language. Respond in JSON format."),
		&info)
	if err != nil {
		log.Fatalf("Unmarshal failed: %v", err)
	}
	fmt.Printf("Capital: %s\n", info.Capital)
	fmt.Printf("Population: %d\n", info.Population)
	fmt.Printf("Language: %s\n\n", info.Language)

	// 4. Streaming
	fmt.Println("4. Streaming Response")
	fmt.Println("---------------------")
	stream, err := client.Stream(ctx,
		gai.Request("openai/gpt-4o-mini").
			User("Count from 1 to 5, one number per line."))
	if err != nil {
		log.Fatalf("Stream failed: %v", err)
	}

	for event := range stream.Events() {
		if event.Type == core.EventTextDelta {
			fmt.Print(event.TextDelta)
		}
	}
	stream.Close()
	fmt.Println()

	// 5. Using aliases
	fmt.Println("\n5. Using Aliases")
	fmt.Println("-----------------")
	aliasClient := gai.NewClient(
		gai.WithAlias("mini", "openai/gpt-4o-mini"),
	)
	text, err = aliasClient.Text(ctx,
		gai.Request("mini").User("Say 'Hello from alias!' and nothing else."))
	if err != nil {
		log.Fatalf("Alias request failed: %v", err)
	}
	fmt.Printf("Response: %s\n\n", text)

	fmt.Println("=== Demo Complete ===")
}
