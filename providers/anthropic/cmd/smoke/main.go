package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "time"

    "github.com/shillcollin/gai/core"
    "github.com/shillcollin/gai/providers/anthropic"
)

func main() {
    key := os.Getenv("ANTHROPIC_API_KEY")
    if key == "" {
        log.Fatal("ANTHROPIC_API_KEY not set")
    }

    client := anthropic.New(
        anthropic.WithAPIKey(key),
        anthropic.WithModel("claude-3-haiku-20240307"),
    )

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    stream, err := client.StreamText(ctx, core.Request{
        Messages: []core.Message{
            core.UserMessage(core.TextPart("Say hello and list one tool you might want to call.")),
        },
        MaxTokens: 128,
        Stream:    true,
    })
    if err != nil {
        log.Fatalf("StreamText error: %v", err)
    }
    defer stream.Close()

    for event := range stream.Events() {
        fmt.Printf("EVENT: %+v\n", event)
    }
    if err := stream.Err(); err != nil {
        log.Fatalf("stream error: %v", err)
    }
}
