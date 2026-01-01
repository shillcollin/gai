// Package main provides a full E2E demo of Vango AI Live Sessions with bidirectional audio.
// This demonstrates real-time voice conversations: speak into your microphone and hear Claude respond.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/vango-ai/vango/pkg/core/types"
	vango "github.com/vango-ai/vango/sdk"
)

const (
	model   = "anthropic/claude-haiku-4-5-20251001"
	voiceID = "f786b574-daa5-4673-aa0c-cbe3e8534c02" // Cartesia default voice

	systemPrompt = ``
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Load .env file
	loadEnvFile()

	// Check for required API keys
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	if anthropicKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY not set")
	}
	cartesiaKey := os.Getenv("CARTESIA_API_KEY")
	if cartesiaKey == "" {
		return fmt.Errorf("CARTESIA_API_KEY not set (required for voice)")
	}

	// Set up signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\n\nGoodbye!")
		cancel()
	}()

	// Print banner
	printBanner()

	// Create SDK client in direct mode
	client := vango.NewClient(
		vango.WithProviderKey("anthropic", anthropicKey),
		vango.WithProviderKey("cartesia", cartesiaKey),
	)

	// Create live session configuration using RunStream with WithLiveConfig
	// This demonstrates the unified API where live mode is accessed via RunStream
	semanticCheck := true
	liveConfig := &vango.LiveConfig{
		Model:  model,
		System: systemPrompt,
		Voice: &vango.LiveVoiceConfig{
			Input: &vango.LiveVoiceInputConfig{
				Provider: "cartesia",
				Language: "en",
			},
			Output: &vango.LiveVoiceOutputConfig{
				Provider: "cartesia",
				Voice:    voiceID,
				Speed:    1.0,
				Format:   "pcm",
			},
			VAD: &vango.LiveVADConfig{
				Model:             "anthropic/claude-haiku-4-5-20251001",
				EnergyThreshold:   0.02,
				SilenceDurationMs: 600,
				SemanticCheck:     &semanticCheck,
			},
			Interrupt: &vango.LiveInterruptConfig{
				Mode:          vango.LiveInterruptModeAuto,
				SemanticCheck: &semanticCheck,
			},
		},
	}

	fmt.Println("Connecting to live session...")

	// Connect via RunStream with WithLiveConfig - the unified streaming API
	stream, err := client.Messages.RunStream(ctx, &vango.MessageRequest{},
		vango.WithLiveConfig(liveConfig),
	)
	if err != nil {
		return fmt.Errorf("connect to live session: %w", err)
	}
	defer stream.Close()

	// Wait for session to be ready by watching for LiveSessionCreatedEvent in Events()
	sessionReady := make(chan string, 1)
	go func() {
		for event := range stream.Events() {
			if wrapper, ok := event.(vango.LiveEventWrapper); ok {
				if e, ok := wrapper.Event.(vango.LiveSessionCreatedEvent); ok {
					sessionReady <- e.SessionID
					return
				}
			}
		}
	}()

	select {
	case sessionID := <-sessionReady:
		fmt.Printf("\n✓ Connected! Session: %s\n", sessionID)
		fmt.Println("\nSpeak into your microphone. Press Ctrl+C to exit.")
		fmt.Println(strings.Repeat("─", 60))
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(10 * time.Second):
		return fmt.Errorf("timeout waiting for session")
	}

	// Start audio player for TTS output
	player := newAudioPlayer(ctx)
	defer player.Close()

	// Start microphone capture
	mic, err := newMicrophoneCapture(ctx)
	if err != nil {
		fmt.Printf("Warning: Could not start microphone: %v\n", err)
		fmt.Println("Falling back to text-only mode. Type your messages below.")
		return runTextMode(ctx, stream, player)
	}
	defer mic.Close()

	// Wire up the audio streams
	var wg sync.WaitGroup

	// Goroutine: Send microphone audio to session
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case chunk, ok := <-mic.Audio():
				if !ok {
					return
				}
				if err := stream.SendAudio(chunk); err != nil {
					return
				}
			}
		}
	}()

	// Goroutine: Handle events from session
	wg.Add(1)
	go func() {
		defer wg.Done()
		handleEvents(ctx, stream, player)
	}()

	// Wait for context cancellation
	<-ctx.Done()
	wg.Wait()

	return nil
}

// handleEvents processes events from the live stream via RunStream.Events()
func handleEvents(ctx context.Context, stream *vango.RunStream, player *audioPlayer) {
	var currentText strings.Builder

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-stream.Events():
			if !ok {
				return
			}

			// Handle wrapped live events
			var liveEvent vango.LiveEvent
			if wrapper, ok := event.(vango.LiveEventWrapper); ok {
				liveEvent = wrapper.Event
			}

			// Handle audio chunks (converted from LiveAudioEvent)
			if audio, ok := event.(vango.AudioChunkEvent); ok {
				player.Write(audio.Data)
				continue
			}

			// Handle text deltas (converted from LiveTextDeltaEvent)
			if wrapper, ok := event.(vango.StreamEventWrapper); ok {
				if delta, ok := wrapper.Event.(types.ContentBlockDeltaEvent); ok {
					if textDelta, ok := delta.Delta.(types.TextDelta); ok {
						currentText.WriteString(textDelta.Text)
						fmt.Print(textDelta.Text)
					}
				}
				continue
			}

			// Handle step complete (converted from LiveMessageStopEvent)
			if _, ok := event.(vango.StepCompleteEvent); ok {
				fmt.Println()
				fmt.Println(strings.Repeat("─", 60))
				continue
			}

			// Handle live-specific events via wrapper
			if liveEvent == nil {
				continue
			}

			switch e := liveEvent.(type) {
			case vango.LiveVADEvent:
				switch e.State {
				case "listening":
					fmt.Print("\r🎤 Listening...                    ")
				case "analyzing":
					fmt.Print("\r🤔 Processing...                   ")
				case "silence":
					// Don't print anything for silence
				}

			case vango.LiveTranscriptEvent:
				if e.Role == "user" {
					if e.IsFinal {
						fmt.Printf("\r\033[K👤 You: %s\n", e.Text)
					} else {
						fmt.Printf("\r👤 You: %s...", e.Text)
					}
				}

			case vango.LiveInputCommittedEvent:
				fmt.Printf("\r\033[K👤 You: %s\n", e.Transcript)
				fmt.Print("🤖 Claude: ")
				currentText.Reset()

			// Note: LiveTextDeltaEvent, LiveAudioEvent, LiveMessageStopEvent
			// are converted to RunStreamEvents and handled above

			case vango.LiveInterruptEvent:
				switch e.State {
				case "detecting":
					fmt.Print("\n[Checking for interrupt...]")
				case "confirmed":
					fmt.Print("\n[Interrupted]\n")
					player.Cancel()
				case "dismissed":
					fmt.Printf("\n[Not an interrupt: %s]\n", e.Reason)
				}

			case vango.LiveToolCallEvent:
				fmt.Printf("\n[Tool: %s]\n", e.Name)

			case vango.LiveErrorEvent:
				fmt.Printf("\n[Error: %s - %s]\n", e.Code, e.Message)
			}
		}
	}
}

// runTextMode provides a fallback text-only mode when microphone isn't available
func runTextMode(ctx context.Context, stream *vango.RunStream, player *audioPlayer) error {
	// Start event handler
	go handleEvents(ctx, stream, player)

	// Read text input
	fmt.Print("\nYou: ")
	var input string
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		_, err := fmt.Scanln(&input)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			continue
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}
		if input == "exit" || input == "quit" {
			return nil
		}

		// Send text and commit
		if err := stream.SendText(input); err != nil {
			return err
		}
		if err := stream.Commit(); err != nil {
			return err
		}

		// Wait a bit for response
		time.Sleep(100 * time.Millisecond)
		fmt.Print("\nYou: ")
	}
}

// --- Microphone Capture ---

type microphoneCapture struct {
	ctx    context.Context
	cancel context.CancelFunc
	cmd    *exec.Cmd
	audio  chan []byte
	done   chan struct{}
}

func newMicrophoneCapture(parentCtx context.Context) (*microphoneCapture, error) {
	ctx, cancel := context.WithCancel(parentCtx)

	// Try different audio capture methods
	var cmd *exec.Cmd

	// Check for sox (rec command) - best option on macOS
	if _, err := exec.LookPath("rec"); err == nil {
		// sox rec: capture at 16kHz 16-bit mono PCM
		cmd = exec.CommandContext(ctx, "rec",
			"-q",        // quiet
			"-t", "raw", // raw PCM output
			"-r", "16000", // 16kHz sample rate (Cartesia STT prefers this)
			"-e", "signed", // signed integers
			"-b", "16", // 16-bit
			"-c", "1", // mono
			"-", // output to stdout
		)
	} else if _, err := exec.LookPath("ffmpeg"); err == nil {
		// ffmpeg: capture from default audio input
		cmd = exec.CommandContext(ctx, "ffmpeg",
			"-f", "avfoundation", // macOS audio capture
			"-i", ":0", // default audio input
			"-ar", "16000", // 16kHz
			"-ac", "1", // mono
			"-f", "s16le", // 16-bit PCM
			"-loglevel", "quiet",
			"-",
		)
	} else if _, err := exec.LookPath("arecord"); err == nil {
		// arecord: Linux ALSA capture
		cmd = exec.CommandContext(ctx, "arecord",
			"-q",
			"-f", "S16_LE",
			"-r", "16000",
			"-c", "1",
			"-t", "raw",
			"-",
		)
	} else {
		cancel()
		return nil, fmt.Errorf("no audio capture tool found (need sox, ffmpeg, or arecord)")
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start audio capture: %w", err)
	}

	mc := &microphoneCapture{
		ctx:    ctx,
		cancel: cancel,
		cmd:    cmd,
		audio:  make(chan []byte, 100),
		done:   make(chan struct{}),
	}

	// Read audio in background
	go func() {
		defer close(mc.done)
		defer close(mc.audio)

		// Read in ~100ms chunks at 16kHz 16-bit mono = 3200 bytes
		buf := make([]byte, 3200)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			n, err := stdout.Read(buf)
			if err != nil {
				return
			}
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				select {
				case mc.audio <- chunk:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return mc, nil
}

func (mc *microphoneCapture) Audio() <-chan []byte {
	return mc.audio
}

func (mc *microphoneCapture) Close() error {
	mc.cancel()
	if mc.cmd.Process != nil {
		mc.cmd.Process.Kill()
	}
	<-mc.done
	return nil
}

// --- Audio Player (Native oto-based) ---

// Audio format constants matching TTS output
const (
	audioSampleRate   = 44100 // 44.1kHz
	audioChannels     = 1     // Mono
	audioBitDepth     = 16    // 16-bit
	audioBytesPerSec  = audioSampleRate * audioChannels * (audioBitDepth / 8)
	audioBufferSizeMs = 100 // Buffer size in milliseconds
)

// audioStreamReader adapts a channel of audio chunks to an io.Reader for oto.
// oto calls Read() from a single goroutine, so no mutex is needed.
type audioStreamReader struct {
	chunks  <-chan []byte
	current []byte
	pos     int
	done    <-chan struct{}
}

func (r *audioStreamReader) Read(p []byte) (n int, err error) {
	// If we have buffered data from a previous chunk, return it
	if r.pos < len(r.current) {
		n = copy(p, r.current[r.pos:])
		r.pos += n
		return n, nil
	}

	// Get next chunk from channel
	select {
	case chunk, ok := <-r.chunks:
		if !ok {
			return 0, io.EOF
		}
		r.current = chunk
		r.pos = 0
		n = copy(p, r.current)
		r.pos = n
		return n, nil
	case <-r.done:
		return 0, io.EOF
	}
}

type audioPlayer struct {
	ctx       context.Context
	cancel    context.CancelFunc
	otoCtx    *oto.Context
	player    *oto.Player
	reader    *audioStreamReader
	chunks    chan []byte
	done      chan struct{}
	wg        sync.WaitGroup
	cancelled bool
	mu        sync.Mutex
}

func newAudioPlayer(parentCtx context.Context) *audioPlayer {
	ctx, cancel := context.WithCancel(parentCtx)
	done := make(chan struct{})
	chunks := make(chan []byte, 100)

	// Create the stream reader that adapts channel to io.Reader
	reader := &audioStreamReader{
		chunks: chunks,
		done:   done,
	}

	// Initialize oto context
	otoCtx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   audioSampleRate,
		ChannelCount: audioChannels,
		Format:       oto.FormatSignedInt16LE,
	})
	if err != nil {
		fmt.Printf("Warning: Failed to initialize audio: %v\n", err)
		cancel()
		return &audioPlayer{
			ctx:    ctx,
			cancel: cancel,
			chunks: chunks,
			done:   done,
		}
	}

	// Wait for audio hardware to be ready
	<-ready

	// Create player from the stream reader
	player := otoCtx.NewPlayer(reader)

	p := &audioPlayer{
		ctx:    ctx,
		cancel: cancel,
		otoCtx: otoCtx,
		player: player,
		reader: reader,
		chunks: chunks,
		done:   done,
	}

	// Start playback - oto will continuously read from our reader
	player.Play()

	return p
}

func (p *audioPlayer) Write(data []byte) {
	p.mu.Lock()
	cancelled := p.cancelled
	p.mu.Unlock()
	if cancelled {
		return
	}
	select {
	case p.chunks <- data:
	case <-p.ctx.Done():
	}
}

func (p *audioPlayer) Cancel() {
	p.mu.Lock()
	if p.cancelled {
		p.mu.Unlock()
		return
	}
	p.cancelled = true
	p.mu.Unlock()

	// Stop playback and drain the channel
	if p.player != nil {
		p.player.Pause()
	}

	// Drain any pending audio chunks
drainLoop:
	for {
		select {
		case <-p.chunks:
		default:
			break drainLoop
		}
	}

	// Resume for next turn
	if p.player != nil {
		p.player.Play()
	}

	// Reset cancelled flag so we can accept new audio
	p.mu.Lock()
	p.cancelled = false
	p.mu.Unlock()
}

func (p *audioPlayer) Close() {
	p.mu.Lock()
	p.cancelled = true
	close(p.done)
	close(p.chunks)
	p.mu.Unlock()

	if p.player != nil {
		p.player.Close()
	}
	p.wg.Wait()
}

// --- Utilities ---

func printBanner() {
	fmt.Println()
	fmt.Println(strings.Repeat("═", 60))
	fmt.Println("  VANGO AI LIVE SESSION DEMO")
	fmt.Println(strings.Repeat("═", 60))
	fmt.Println()
	fmt.Println("  Real-time bidirectional voice conversation with Claude.")
	fmt.Println()
	fmt.Println("  • Speak naturally into your microphone")
	fmt.Println("  • Claude will respond with voice")
	fmt.Println("  • Interrupt anytime by speaking")
	fmt.Println()
	fmt.Println(strings.Repeat("─", 60))
}

func loadEnvFile() {
	dir, _ := os.Getwd()
	for i := 0; i < 5; i++ {
		envPath := filepath.Join(dir, ".env")
		if data, err := os.ReadFile(envPath); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				if idx := strings.Index(line, "="); idx > 0 {
					key := strings.TrimSpace(line[:idx])
					value := strings.TrimSpace(line[idx+1:])
					if os.Getenv(key) == "" {
						os.Setenv(key, value)
					}
				}
			}
			return
		}
		dir = filepath.Dir(dir)
	}
}
