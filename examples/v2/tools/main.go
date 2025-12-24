// Package main demonstrates tool/function calling with the GAI v2 client.
package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"time"

	"github.com/shillcollin/gai"
	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/tools"
	_ "github.com/shillcollin/gai/providers/openai"
)

func main() {
	client := gai.NewClient()

	if !client.HasProvider("openai") {
		log.Fatal("OpenAI provider not available. Set OPENAI_API_KEY environment variable.")
	}

	ctx := context.Background()

	// Define tools using the tools package
	calcTool := tools.New(
		"calculator",
		"Performs basic arithmetic operations",
		func(ctx context.Context, input struct {
			Expression string `json:"expression" description:"The math expression to evaluate (e.g., '2 + 2', '10 * 5')"`
		}, meta core.ToolMeta) (string, error) {
			// Simple expression evaluator (in real code, use a proper expression parser)
			result := evaluateSimple(input.Expression)
			return fmt.Sprintf("Result: %v", result), nil
		},
	)

	weatherTool := tools.New(
		"get_weather",
		"Gets the current weather for a location",
		func(ctx context.Context, input struct {
			Location string `json:"location" description:"The city and country (e.g., 'Paris, France')"`
		}, meta core.ToolMeta) (map[string]any, error) {
			// Simulated weather data
			return map[string]any{
				"location":    input.Location,
				"temperature": 22,
				"unit":        "celsius",
				"condition":   "sunny",
				"humidity":    45,
			}, nil
		},
	)

	timeTool := tools.New(
		"get_time",
		"Gets the current time in a timezone",
		func(ctx context.Context, input struct {
			Timezone string `json:"timezone" description:"The timezone (e.g., 'America/New_York', 'Europe/London')"`
		}, meta core.ToolMeta) (string, error) {
			loc, err := time.LoadLocation(input.Timezone)
			if err != nil {
				return "", fmt.Errorf("unknown timezone: %s", input.Timezone)
			}
			return time.Now().In(loc).Format("3:04 PM on Monday, January 2, 2006"), nil
		},
	)

	// Create tool adapters for the core interface
	toolHandles := []core.ToolHandle{
		tools.NewCoreAdapter(calcTool),
		tools.NewCoreAdapter(weatherTool),
		tools.NewCoreAdapter(timeTool),
	}

	// Run an agentic loop with tools
	result, err := client.Run(ctx,
		gai.Request("openai/gpt-4o").
			System("You are a helpful assistant with access to tools. Use them when needed to answer questions.").
			User("What's the weather like in Tokyo, Japan? Also, what time is it there?").
			Tools(toolHandles...).
			MaxSteps(5))
	if err != nil {
		log.Fatalf("Run failed: %v", err)
	}

	fmt.Println("--- Tool Execution Result ---")
	fmt.Println("Response:", result.Text())
	fmt.Println("\nExecution details:")
	fmt.Printf("  Steps: %d\n", result.StepCount())
	fmt.Printf("  Tool calls: %d\n", len(result.ToolCalls()))

	// Show tool calls made
	for i, call := range result.ToolCalls() {
		fmt.Printf("  Call %d: %s\n", i+1, call.Name)
	}

	fmt.Printf("\nTokens used: %d\n", result.TotalTokens())
	fmt.Printf("Finish reason: %s\n", result.FinishReason().Type)

	// Example with calculator
	fmt.Println("\n--- Calculator Example ---")
	result2, err := client.Run(ctx,
		gai.Request("openai/gpt-4o").
			User("What is 15% of 847? Also calculate the square root of 144.").
			Tools(tools.NewCoreAdapter(calcTool)).
			StopWhen(core.NoMoreTools()))
	if err != nil {
		log.Fatalf("Run failed: %v", err)
	}

	fmt.Println("Response:", result2.Text())
	for _, call := range result2.ToolCalls() {
		fmt.Printf("Used tool: %s\n", call.Name)
	}

	os.Exit(0)
}

// evaluateSimple is a basic expression evaluator for demo purposes.
func evaluateSimple(expr string) float64 {
	// This is a simplified demo - in production use a proper expression parser
	var a, b float64
	var op string

	if _, err := fmt.Sscanf(expr, "%f %s %f", &a, &op, &b); err == nil {
		switch op {
		case "+":
			return a + b
		case "-":
			return a - b
		case "*":
			return a * b
		case "/":
			if b != 0 {
				return a / b
			}
		case "%":
			return float64(int(a) % int(b))
		}
	}

	// Handle special functions
	var n float64
	if count, _ := fmt.Sscanf(expr, "sqrt(%f)", &n); count == 1 {
		return math.Sqrt(n)
	}

	return 0
}
