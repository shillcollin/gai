// Package main demonstrates structured output generation with the GAI v2 client.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/shillcollin/gai"
	_ "github.com/shillcollin/gai/providers/openai"
)

// Recipe represents a cooking recipe with structured fields.
type Recipe struct {
	Name        string   `json:"name"`
	PrepTime    string   `json:"prep_time"`
	CookTime    string   `json:"cook_time"`
	Servings    int      `json:"servings"`
	Ingredients []string `json:"ingredients"`
	Steps       []string `json:"steps"`
	Tips        []string `json:"tips,omitempty"`
}

// Person represents basic person information.
type Person struct {
	Name       string `json:"name"`
	Age        int    `json:"age"`
	Occupation string `json:"occupation"`
	Location   string `json:"location"`
}

func main() {
	client := gai.NewClient()

	if !client.HasProvider("openai") {
		log.Fatal("OpenAI provider not available. Set OPENAI_API_KEY environment variable.")
	}

	ctx := context.Background()

	// Generate a structured recipe
	var recipe Recipe
	err := client.Unmarshal(ctx,
		gai.Request("openai/gpt-4o").
			System("You are a professional chef. Provide recipes in structured format.").
			User("Give me a simple chocolate chip cookie recipe."),
		&recipe)
	if err != nil {
		log.Fatalf("Unmarshal failed: %v", err)
	}

	fmt.Println("--- Chocolate Chip Cookie Recipe ---")
	fmt.Printf("Name: %s\n", recipe.Name)
	fmt.Printf("Prep Time: %s\n", recipe.PrepTime)
	fmt.Printf("Cook Time: %s\n", recipe.CookTime)
	fmt.Printf("Servings: %d\n", recipe.Servings)
	fmt.Println("\nIngredients:")
	for _, ing := range recipe.Ingredients {
		fmt.Printf("  - %s\n", ing)
	}
	fmt.Println("\nSteps:")
	for i, step := range recipe.Steps {
		fmt.Printf("  %d. %s\n", i+1, step)
	}
	if len(recipe.Tips) > 0 {
		fmt.Println("\nTips:")
		for _, tip := range recipe.Tips {
			fmt.Printf("  - %s\n", tip)
		}
	}

	// Generate a fictional person
	var person Person
	err = client.Unmarshal(ctx,
		gai.Request("openai/gpt-4o-mini").
			User("Generate a fictional person with name, age, occupation, and location."),
		&person)
	if err != nil {
		log.Fatalf("Unmarshal failed: %v", err)
	}

	fmt.Println("\n--- Generated Person ---")
	fmt.Printf("Name: %s\n", person.Name)
	fmt.Printf("Age: %d\n", person.Age)
	fmt.Printf("Occupation: %s\n", person.Occupation)
	fmt.Printf("Location: %s\n", person.Location)

	// Generate a list of items
	var cities []string
	err = client.Unmarshal(ctx,
		gai.Request("openai/gpt-4o-mini").
			User("List the 5 largest cities in Japan by population."),
		&cities)
	if err != nil {
		log.Fatalf("Unmarshal failed: %v", err)
	}

	fmt.Println("\n--- Largest Cities in Japan ---")
	for i, city := range cities {
		fmt.Printf("  %d. %s\n", i+1, city)
	}

	os.Exit(0)
}
