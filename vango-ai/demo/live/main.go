// Package main provides a minimal CLI demo for live voice conversations.
//
// This demonstrates the intended simple DX for Vango AI live sessions.
//
// Usage:
//
//	go run demo/live/main.go
//
// Environment variables:
//
//	ANTHROPIC_API_KEY - Required for LLM
//	CARTESIA_API_KEY  - Required for STT and TTS
//
// Controls:
//
//	q + ENTER - Quit the demo
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/joho/godotenv"

	vango "github.com/vango-ai/vango/sdk"
)

func main() {
	_ = godotenv.Load()

	// Validate API keys
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		log.Fatal("ANTHROPIC_API_KEY required")
	}
	if os.Getenv("CARTESIA_API_KEY") == "" {
		log.Fatal("CARTESIA_API_KEY required")
	}

	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║              Vango AI Live Voice Demo                      ║")
	fmt.Println("╠════════════════════════════════════════════════════════════╣")
	fmt.Println("║  Speak naturally - automatic turn detection is enabled.    ║")
	fmt.Println("║  Press 'q' + ENTER to quit.                                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
	fmt.Println()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nShutting down...")
		cancel()
	}()

	// Create Vango client
	client := vango.NewClient()

	// Start live session with minimal config
	session, err := client.Live(ctx, vango.LiveConfig{
		Model:  "anthropic/claude-haiku-4-5-20251001",
		System: "You are in live voice mode right now (via STT <> TTS). This means you need to be more conversational in your responses. Sometimes a single word reply (e.g. “okay”, “uh huh?”) is appropriate. If you received the lastest user message, but you’re not sure if they are actually done talking (e.g. they are trying to explain something and paused to think), you can give a short reply to help gauge. (e.g. “ya?”, “uh huh?”, “Are you done explaining? I have some thoughts..”, etc..)",
		Debug:  true, // Enable debug logs
	})
	if err != nil {
		log.Fatalf("Failed to start live session: %v", err)
	}
	defer session.Close()

	// Initialize audio I/O
	mic, speaker, cleanup := initAudio()
	defer cleanup()

	// Send microphone audio to session
	go func() {
		buf := make([]byte, sampleRate*2/50) // 20ms chunks
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			n := mic.Read(buf)
			if n > 0 {
				session.SendAudio(buf[:n])
			}
		}
	}()

	// Handle session events
	go func() {
		for event := range session.Events() {
			switch e := event.(type) {
			case *vango.LiveAudioDeltaEvent:
				// Play TTS audio
				speaker.Write(e.Data)

			case *vango.LiveAudioFlushEvent:
				// User continued speaking during grace period or interrupted
				// Immediately discard buffered audio
				speaker.Flush()

			case *vango.LiveErrorEvent:
				fmt.Printf("\n[ERROR] %s: %s\n", e.Code, e.Message)
			}
		}
	}()

	// Wait for quit command
	fmt.Println("Listening... (press 'q' + ENTER to quit)")
	var input string
	for {
		fmt.Scanln(&input)
		if strings.ToLower(strings.TrimSpace(input)) == "q" {
			break
		}
	}
}
