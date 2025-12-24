// Voice Chat Example
//
// A CLI chatbot with real audio input/output on macOS.
//
// Prerequisites:
//
//	brew install portaudio
//
// Usage:
//
//	1. Copy .env.example to .env and add your API keys
//	2. go run main.go
//
// Commands:
//
//	/voice   - Toggle voice mode (TTS output)
//	/record  - Record from microphone (press Enter to stop)
//	/file    - Send audio file as input
//	/clear   - Clear conversation history
//	/history - Show conversation history
//	/quit    - Exit the chatbot
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/gordonklaus/portaudio"
	"github.com/joho/godotenv"
	"github.com/shillcollin/gai"
	"github.com/shillcollin/gai/core"

	// Import providers
	_ "github.com/shillcollin/gai/providers/anthropic"
	_ "github.com/shillcollin/gai/providers/openai"

	// Import voice providers (optional - will work without them)
	_ "github.com/shillcollin/gai/stt/deepgram"
	_ "github.com/shillcollin/gai/tts/elevenlabs"
)

const (
	sampleRate = 16000
	channels   = 1
)

func main() {
	// Load .env file (optional - won't error if missing)
	_ = godotenv.Load()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize PortAudio
	if err := portaudio.Initialize(); err != nil {
		fmt.Printf("Warning: Could not initialize audio: %v\n", err)
	} else {
		defer portaudio.Terminate()
	}

	// Handle Ctrl+C
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nGoodbye!")
		cancel()
		os.Exit(0)
	}()

	// Create client
	client := gai.NewClient()

	// Determine which model to use based on available API keys
	model := detectModel()
	if model == "" {
		fmt.Println("Error: No API key found. Please set ANTHROPIC_API_KEY or OPENAI_API_KEY")
		os.Exit(1)
	}

	// Detect voice capabilities
	voice := detectVoice()
	stt := detectSTT()

	// Create conversation
	convOpts := []gai.ConversationOption{
		gai.ConvModel(model),
		gai.ConvSystem("You are a friendly and helpful voice assistant. Keep responses concise and conversational - ideally 1-3 sentences unless the user asks for more detail."),
	}
	if voice != "" {
		convOpts = append(convOpts, gai.ConvVoice(voice))
	}
	if stt != "" {
		convOpts = append(convOpts, gai.ConvSTT(stt))
	}

	conv := client.Conversation(convOpts...)

	// State
	voiceMode := voice != "" // Default to voice mode if available

	// Print welcome message
	printWelcome(model, voice, stt, voiceMode)

	// Main loop
	scanner := bufio.NewScanner(os.Stdin)
	for {
		// Print prompt
		if voiceMode {
			fmt.Print("\n🎤 You (voice mode): ")
		} else {
			fmt.Print("\n💬 You: ")
		}

		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		// Handle commands
		if strings.HasPrefix(input, "/") {
			switch strings.ToLower(input) {
			case "/voice":
				if voice == "" {
					fmt.Println("❌ Voice mode unavailable. Set ELEVENLABS_API_KEY for TTS.")
				} else {
					voiceMode = !voiceMode
					if voiceMode {
						fmt.Println("🔊 Voice mode ON - responses will be spoken")
					} else {
						fmt.Println("🔇 Voice mode OFF - text responses only")
					}
				}
				continue

			case "/record", "/r":
				if stt == "" {
					fmt.Println("❌ Audio input unavailable. Set DEEPGRAM_API_KEY for STT.")
				} else {
					audioData, err := recordAudio()
					if err != nil {
						fmt.Printf("❌ Recording error: %v\n", err)
						continue
					}
					if err := handleAudioInput(ctx, client, conv, audioData, voiceMode); err != nil {
						if ctx.Err() != nil {
							return
						}
						fmt.Printf("❌ Error: %v\n", err)
					}
				}
				continue

			case "/file":
				fmt.Println("📁 Enter path to audio file (WAV/MP3):")
				fmt.Print("   > ")
				if scanner.Scan() {
					audioPath := strings.TrimSpace(scanner.Text())
					if err := handleAudioFile(ctx, client, conv, audioPath, voiceMode); err != nil {
						fmt.Printf("❌ Error: %v\n", err)
					}
				}
				continue

			case "/clear":
				conv.Clear()
				fmt.Println("🗑️  Conversation cleared")
				continue

			case "/history":
				printHistory(conv)
				continue

			case "/quit", "/exit", "/q":
				fmt.Println("👋 Goodbye!")
				return

			case "/help":
				printHelp()
				continue

			default:
				fmt.Printf("❓ Unknown command: %s (type /help for commands)\n", input)
				continue
			}
		}

		// Send text message
		if err := handleTextInput(ctx, client, conv, input, voiceMode); err != nil {
			if ctx.Err() != nil {
				return
			}
			fmt.Printf("❌ Error: %v\n", err)
		}
	}
}

func handleTextInput(ctx context.Context, client *gai.Client, conv *gai.Conversation, input string, withVoice bool) error {
	fmt.Print("\n🤖 Assistant: ")

	// Use Say for voice mode (need full response for TTS), Stream for text mode
	if withVoice {
		result, err := conv.Say(ctx, input)
		if err != nil {
			return err
		}

		fmt.Println(result.Text())

		// Play audio
		if result.HasAudio() {
			fmt.Print("🔊 Speaking... ")
			if err := playAudio(result.AudioData(), result.AudioMIME()); err != nil {
				fmt.Printf("(playback error: %v)\n", err)
			} else {
				fmt.Println("✓")
			}
		}
	} else {
		// Stream the response for text mode
		stream, err := conv.Stream(ctx, input)
		if err != nil {
			return err
		}

		for event := range stream.Events() {
			if event.Type == core.EventTextDelta {
				fmt.Print(event.TextDelta)
			}
		}
		fmt.Println()

		if err := stream.Err(); err != nil && err != core.ErrStreamClosed {
			return err
		}
		stream.Close()
	}

	return nil
}

func handleAudioInput(ctx context.Context, client *gai.Client, conv *gai.Conversation, audioData []byte, withVoice bool) error {
	fmt.Printf("📤 Processing audio (%d bytes)...\n", len(audioData))

	// Use GenerateVoice directly for the full voice pipeline
	result, err := client.GenerateVoice(ctx,
		gai.Request(conv.Model()).
			System(conv.System()).
			Messages(conv.Messages()...).
			Audio(audioData, "audio/wav").
			STT("deepgram/nova-2").
			Voice(func() string {
				if withVoice {
					return "elevenlabs/rachel"
				}
				return ""
			}()))
	if err != nil {
		return err
	}

	// Update conversation history manually
	if result.HasTranscript() {
		conv.AddMessages(core.UserMessage(core.Text{Text: result.Transcript()}))
	}
	conv.AddMessages(result.Messages()...)

	// Print transcript
	if result.HasTranscript() {
		fmt.Printf("\n📝 You said: %s\n", result.Transcript())
	}

	fmt.Printf("🤖 Assistant: %s\n", result.Text())

	// Play audio response
	if withVoice && result.HasAudio() {
		fmt.Print("🔊 Speaking... ")
		if err := playAudio(result.AudioData(), result.AudioMIME()); err != nil {
			fmt.Printf("(playback error: %v)\n", err)
		} else {
			fmt.Println("✓")
		}
	}

	return nil
}

func handleAudioFile(ctx context.Context, client *gai.Client, conv *gai.Conversation, audioPath string, withVoice bool) error {
	// Read audio file
	audioData, err := os.ReadFile(audioPath)
	if err != nil {
		return fmt.Errorf("failed to read audio file: %w", err)
	}

	// Detect MIME type from extension
	mime := "audio/wav"
	lower := strings.ToLower(audioPath)
	if strings.HasSuffix(lower, ".mp3") {
		mime = "audio/mp3"
	} else if strings.HasSuffix(lower, ".m4a") {
		mime = "audio/m4a"
	} else if strings.HasSuffix(lower, ".ogg") {
		mime = "audio/ogg"
	}

	fmt.Printf("📤 Processing audio file (%d bytes, %s)...\n", len(audioData), mime)

	// Use GenerateVoice for full pipeline
	result, err := client.GenerateVoice(ctx,
		gai.Request(conv.Model()).
			System(conv.System()).
			Messages(conv.Messages()...).
			Audio(audioData, mime).
			STT("deepgram/nova-2").
			Voice(func() string {
				if withVoice {
					return "elevenlabs/rachel"
				}
				return ""
			}()))
	if err != nil {
		return err
	}

	// Update conversation history
	if result.HasTranscript() {
		conv.AddMessages(core.UserMessage(core.Text{Text: result.Transcript()}))
	}
	conv.AddMessages(result.Messages()...)

	if result.HasTranscript() {
		fmt.Printf("\n📝 You said: %s\n", result.Transcript())
	}

	fmt.Printf("🤖 Assistant: %s\n", result.Text())

	if withVoice && result.HasAudio() {
		fmt.Print("🔊 Speaking... ")
		if err := playAudio(result.AudioData(), result.AudioMIME()); err != nil {
			fmt.Printf("(playback error: %v)\n", err)
		} else {
			fmt.Println("✓")
		}
	}

	return nil
}

// recordAudio records from the microphone until Enter is pressed
func recordAudio() ([]byte, error) {
	fmt.Println("🎙️  Recording... (press Enter to stop)")

	// Create buffer for audio data
	var audioBuffer bytes.Buffer
	var mu sync.Mutex
	recording := true

	// Audio callback buffer
	inputBuffer := make([]int16, 512)

	// Open input stream
	stream, err := portaudio.OpenDefaultStream(
		channels,    // input channels
		0,           // output channels
		float64(sampleRate),
		len(inputBuffer),
		func(in []int16) {
			mu.Lock()
			defer mu.Unlock()
			if recording {
				// Write samples to buffer
				for _, sample := range in {
					binary.Write(&audioBuffer, binary.LittleEndian, sample)
				}
			}
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to open audio stream: %w", err)
	}
	defer stream.Close()

	// Start recording
	if err := stream.Start(); err != nil {
		return nil, fmt.Errorf("failed to start recording: %w", err)
	}

	// Wait for Enter key
	reader := bufio.NewReader(os.Stdin)
	reader.ReadString('\n')

	// Stop recording
	mu.Lock()
	recording = false
	mu.Unlock()

	if err := stream.Stop(); err != nil {
		return nil, fmt.Errorf("failed to stop recording: %w", err)
	}

	// Get raw PCM data
	pcmData := audioBuffer.Bytes()
	if len(pcmData) == 0 {
		return nil, fmt.Errorf("no audio recorded")
	}

	// Convert to WAV
	wavData := pcmToWav(pcmData, sampleRate, channels)

	fmt.Printf("✓ Recorded %.1f seconds\n", float64(len(pcmData))/(float64(sampleRate)*2))

	return wavData, nil
}

// pcmToWav wraps raw PCM data in a WAV header
func pcmToWav(pcmData []byte, sampleRate, channels int) []byte {
	var buf bytes.Buffer

	// RIFF header
	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, uint32(36+len(pcmData))) // file size - 8
	buf.WriteString("WAVE")

	// fmt chunk
	buf.WriteString("fmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16))              // chunk size
	binary.Write(&buf, binary.LittleEndian, uint16(1))               // audio format (PCM)
	binary.Write(&buf, binary.LittleEndian, uint16(channels))        // channels
	binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))      // sample rate
	binary.Write(&buf, binary.LittleEndian, uint32(sampleRate*channels*2)) // byte rate
	binary.Write(&buf, binary.LittleEndian, uint16(channels*2))      // block align
	binary.Write(&buf, binary.LittleEndian, uint16(16))              // bits per sample

	// data chunk
	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, uint32(len(pcmData)))
	buf.Write(pcmData)

	return buf.Bytes()
}

// playAudio plays audio using afplay (macOS) or ffplay
func playAudio(audioData []byte, mimeType string) error {
	// Determine file extension from MIME type
	ext := ".mp3"
	switch {
	case strings.Contains(mimeType, "wav"):
		ext = ".wav"
	case strings.Contains(mimeType, "ogg"):
		ext = ".ogg"
	case strings.Contains(mimeType, "aac"), strings.Contains(mimeType, "m4a"):
		ext = ".m4a"
	}

	// Write to temp file
	tmpFile, err := os.CreateTemp("", "gai-audio-*"+ext)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(audioData); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write audio: %w", err)
	}
	tmpFile.Close()

	// Try afplay (macOS native)
	cmd := exec.Command("afplay", tmpFile.Name())
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Run(); err != nil {
		// Fall back to ffplay
		cmd = exec.Command("ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet", tmpFile.Name())
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("no audio player available (tried afplay, ffplay)")
		}
	}

	return nil
}

func printHistory(conv *gai.Conversation) {
	msgs := conv.Messages()
	if len(msgs) == 0 {
		fmt.Println("📜 No messages in history")
		return
	}

	fmt.Println("\n📜 Conversation History:")
	fmt.Println(strings.Repeat("-", 40))

	for i, msg := range msgs {
		role := string(msg.Role)
		switch msg.Role {
		case core.User:
			role = "👤 User"
		case core.Assistant:
			role = "🤖 Assistant"
		case core.System:
			role = "⚙️  System"
		}

		// Get text content
		var text string
		for _, part := range msg.Parts {
			if t, ok := part.(core.Text); ok {
				text = t.Text
				break
			}
		}

		// Truncate long messages
		if len(text) > 100 {
			text = text[:100] + "..."
		}

		fmt.Printf("%d. %s: %s\n", i+1, role, text)
	}
	fmt.Println(strings.Repeat("-", 40))
}

func printWelcome(model, voice, stt string, voiceMode bool) {
	fmt.Println()
	fmt.Println("╔════════════════════════════════════════╗")
	fmt.Println("║       🤖 Voice Chat Assistant 🎤       ║")
	fmt.Println("╚════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("  Model: %s\n", model)
	if voice != "" {
		fmt.Printf("  Voice: %s ✅\n", voice)
	} else {
		fmt.Println("  Voice: Not configured (set ELEVENLABS_API_KEY)")
	}
	if stt != "" {
		fmt.Printf("  STT:   %s ✅\n", stt)
	} else {
		fmt.Println("  STT:   Not configured (set DEEPGRAM_API_KEY)")
	}
	fmt.Println()
	if voiceMode {
		fmt.Println("  🔊 Voice mode is ON")
	} else {
		fmt.Println("  🔇 Voice mode is OFF")
	}
	fmt.Println()
	fmt.Println("  Commands:")
	fmt.Println("    /record  - Record from microphone")
	fmt.Println("    /voice   - Toggle voice output")
	fmt.Println("    /file    - Send audio file")
	fmt.Println("    /clear   - Clear history")
	fmt.Println("    /history - Show history")
	fmt.Println("    /quit    - Exit")
	fmt.Println()
	fmt.Println("  Type a message or use /record to speak!")
	fmt.Println()
}

func printHelp() {
	fmt.Println()
	fmt.Println("Available commands:")
	fmt.Println("  /record, /r - Record from microphone (press Enter to stop)")
	fmt.Println("  /voice      - Toggle voice mode (TTS for responses)")
	fmt.Println("  /file       - Send an audio file as input")
	fmt.Println("  /clear      - Clear conversation history")
	fmt.Println("  /history    - Show conversation history")
	fmt.Println("  /help       - Show this help message")
	fmt.Println("  /quit       - Exit the chatbot")
	fmt.Println()
}

func detectModel() string {
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		return "anthropic/claude-sonnet-4-20250514"
	}
	if os.Getenv("OPENAI_API_KEY") != "" {
		return "openai/gpt-4o"
	}
	return ""
}

func detectVoice() string {
	if os.Getenv("ELEVENLABS_API_KEY") != "" {
		return "elevenlabs/rachel"
	}
	return ""
}

func detectSTT() string {
	if os.Getenv("DEEPGRAM_API_KEY") != "" {
		return "deepgram/nova-2"
	}
	return ""
}
