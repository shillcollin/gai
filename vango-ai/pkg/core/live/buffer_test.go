package live

import (
	"encoding/binary"
	"math"
	"testing"
	"time"
)

// generatePCM creates test PCM audio data with a specific RMS energy level.
// The energy is approximately the target value (not exact due to discretization).
func generatePCM(targetEnergy float64, numSamples int) []byte {
	// RMS = sqrt(sum(x^2) / n)
	// For constant amplitude: RMS = amplitude
	// So we want amplitude = targetEnergy * 32768 (for normalized to [-1,1])
	amplitude := targetEnergy * 32768
	if amplitude > 32767 {
		amplitude = 32767
	}

	pcm := make([]byte, numSamples*2)
	sample := int16(amplitude)

	for i := 0; i < numSamples; i++ {
		// Alternate positive/negative to simulate a simple waveform
		if i%2 == 0 {
			binary.LittleEndian.PutUint16(pcm[i*2:], uint16(sample))
		} else {
			binary.LittleEndian.PutUint16(pcm[i*2:], uint16(-sample))
		}
	}

	return pcm
}

// generateSilence creates PCM data with near-zero energy.
func generateSilence(numSamples int) []byte {
	return make([]byte, numSamples*2) // All zeros
}

// generateSine creates a sine wave with specific amplitude.
func generateSine(amplitude float64, frequency float64, sampleRate int, duration time.Duration) []byte {
	numSamples := int(float64(sampleRate) * duration.Seconds())
	pcm := make([]byte, numSamples*2)

	for i := 0; i < numSamples; i++ {
		t := float64(i) / float64(sampleRate)
		sample := int16(amplitude * 32768 * math.Sin(2*math.Pi*frequency*t))
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(sample))
	}

	return pcm
}

func TestDefaultAudioFormat(t *testing.T) {
	f := DefaultAudioFormat()

	if f.SampleRate != 24000 {
		t.Errorf("expected SampleRate 24000, got %d", f.SampleRate)
	}
	if f.Channels != 1 {
		t.Errorf("expected Channels 1, got %d", f.Channels)
	}
	if f.BitDepth != 16 {
		t.Errorf("expected BitDepth 16, got %d", f.BitDepth)
	}
}

func TestAudioFormat_BytesPerSecond(t *testing.T) {
	f := DefaultAudioFormat()
	// 24000 samples/sec * 1 channel * 2 bytes/sample = 48000 bytes/sec
	expected := 48000
	if f.BytesPerSecond() != expected {
		t.Errorf("expected BytesPerSecond %d, got %d", expected, f.BytesPerSecond())
	}
}

func TestAudioFormat_DurationToBytes(t *testing.T) {
	f := DefaultAudioFormat()

	// 100ms at 48000 bytes/sec = 4800 bytes
	d := 100 * time.Millisecond
	expected := 4800
	if f.DurationToBytes(d) != expected {
		t.Errorf("expected DurationToBytes(%v) = %d, got %d", d, expected, f.DurationToBytes(d))
	}

	// 1 second = 48000 bytes
	d = time.Second
	expected = 48000
	if f.DurationToBytes(d) != expected {
		t.Errorf("expected DurationToBytes(%v) = %d, got %d", d, expected, f.DurationToBytes(d))
	}
}

func TestAudioFormat_BytesToDuration(t *testing.T) {
	f := DefaultAudioFormat()

	// 48000 bytes = 1 second
	bytes := 48000
	expected := time.Second
	if f.BytesToDuration(bytes) != expected {
		t.Errorf("expected BytesToDuration(%d) = %v, got %v", bytes, expected, f.BytesToDuration(bytes))
	}

	// 4800 bytes = 100ms
	bytes = 4800
	expected = 100 * time.Millisecond
	if f.BytesToDuration(bytes) != expected {
		t.Errorf("expected BytesToDuration(%d) = %v, got %v", bytes, expected, f.BytesToDuration(bytes))
	}
}

func TestCalculateEnergy_Silence(t *testing.T) {
	silence := generateSilence(1000)
	energy := CalculateEnergy(silence)

	if energy != 0 {
		t.Errorf("expected silence energy = 0, got %f", energy)
	}
}

func TestCalculateEnergy_MaxAmplitude(t *testing.T) {
	// Full amplitude signal
	pcm := make([]byte, 2000)
	negMax := int16(-32768)
	for i := 0; i < 1000; i++ {
		if i%2 == 0 {
			binary.LittleEndian.PutUint16(pcm[i*2:], uint16(32767))
		} else {
			binary.LittleEndian.PutUint16(pcm[i*2:], uint16(negMax))
		}
	}

	energy := CalculateEnergy(pcm)

	// Should be close to 1.0 (max normalized amplitude)
	if energy < 0.99 {
		t.Errorf("expected max amplitude energy ~1.0, got %f", energy)
	}
}

func TestCalculateEnergy_HalfAmplitude(t *testing.T) {
	// Half amplitude signal (constant 16384)
	pcm := make([]byte, 2000)
	negHalf := int16(-16384)
	for i := 0; i < 1000; i++ {
		if i%2 == 0 {
			binary.LittleEndian.PutUint16(pcm[i*2:], 16384)
		} else {
			binary.LittleEndian.PutUint16(pcm[i*2:], uint16(negHalf))
		}
	}

	energy := CalculateEnergy(pcm)

	// Should be approximately 0.5
	if energy < 0.45 || energy > 0.55 {
		t.Errorf("expected half amplitude energy ~0.5, got %f", energy)
	}
}

func TestAudioBuffer_Write(t *testing.T) {
	format := DefaultAudioFormat()
	buf := NewAudioBuffer(format, time.Second)

	pcm := generatePCM(0.1, 1000)
	buf.Write(pcm)

	if buf.TotalBytesReceived() != int64(len(pcm)) {
		t.Errorf("expected TotalBytesReceived %d, got %d", len(pcm), buf.TotalBytesReceived())
	}

	if buf.DataLength() != len(pcm) {
		t.Errorf("expected DataLength %d, got %d", len(pcm), buf.DataLength())
	}
}

func TestAudioBuffer_LastEnergy(t *testing.T) {
	format := DefaultAudioFormat()
	buf := NewAudioBuffer(format, time.Second)

	// Write silence
	silence := generateSilence(1000)
	buf.Write(silence)
	if buf.LastEnergy() != 0 {
		t.Errorf("expected LastEnergy 0 for silence, got %f", buf.LastEnergy())
	}

	// Write speech
	speech := generatePCM(0.1, 1000)
	buf.Write(speech)
	energy := buf.LastEnergy()
	if energy < 0.05 || energy > 0.15 {
		t.Errorf("expected LastEnergy ~0.1, got %f", energy)
	}
}

func TestAudioBuffer_IsSilent(t *testing.T) {
	format := DefaultAudioFormat()
	buf := NewAudioBuffer(format, time.Second)
	threshold := 0.02

	// Should be silent initially (no data)
	if !buf.IsSilent(threshold) {
		t.Error("expected IsSilent=true with no data")
	}

	// Write silence
	silence := generateSilence(1000)
	buf.Write(silence)
	if !buf.IsSilent(threshold) {
		t.Error("expected IsSilent=true after writing silence")
	}

	// Write speech
	speech := generatePCM(0.1, 1000)
	buf.Write(speech)
	if buf.IsSilent(threshold) {
		t.Error("expected IsSilent=false after writing speech")
	}
}

func TestAudioBuffer_HasSustainedEnergy(t *testing.T) {
	format := DefaultAudioFormat()
	buf := NewAudioBuffer(format, time.Second)
	threshold := 0.02
	duration := 100 * time.Millisecond

	// Write several chunks of speech over time
	speech := generatePCM(0.1, 1000)
	for i := 0; i < 5; i++ {
		buf.Write(speech)
		time.Sleep(25 * time.Millisecond)
	}

	// Should have sustained energy
	if !buf.HasSustainedEnergy(threshold, duration) {
		t.Error("expected HasSustainedEnergy=true after sustained speech")
	}

	// Write silence - should break the sustained period
	silence := generateSilence(1000)
	buf.Write(silence)
	if buf.HasSustainedEnergy(threshold, duration) {
		t.Error("expected HasSustainedEnergy=false after silence")
	}
}

func TestAudioBuffer_SilenceDuration(t *testing.T) {
	format := DefaultAudioFormat()
	buf := NewAudioBuffer(format, time.Second)
	threshold := 0.02

	// Write speech first
	speech := generatePCM(0.1, 1000)
	buf.Write(speech)

	// Should be 0 when not silent
	if buf.SilenceDuration(threshold) != 0 {
		t.Error("expected SilenceDuration=0 during speech")
	}

	// Write silence
	silence := generateSilence(1000)
	buf.Write(silence)
	time.Sleep(50 * time.Millisecond)
	buf.Write(silence)
	time.Sleep(50 * time.Millisecond)
	buf.Write(silence)

	// Should track silence duration
	dur := buf.SilenceDuration(threshold)
	if dur < 100*time.Millisecond {
		t.Errorf("expected SilenceDuration >= 100ms, got %v", dur)
	}
}

func TestAudioBuffer_Reset(t *testing.T) {
	format := DefaultAudioFormat()
	buf := NewAudioBuffer(format, time.Second)

	speech := generatePCM(0.1, 1000)
	buf.Write(speech)

	if buf.TotalBytesReceived() == 0 {
		t.Error("expected bytes received before reset")
	}

	buf.Reset()

	if buf.TotalBytesReceived() != 0 {
		t.Error("expected TotalBytesReceived=0 after reset")
	}
	if buf.DataLength() != 0 {
		t.Error("expected DataLength=0 after reset")
	}
	if buf.LastEnergy() != 0 {
		t.Error("expected LastEnergy=0 after reset")
	}
}

func TestAudioBuffer_GetRecentData(t *testing.T) {
	format := DefaultAudioFormat()
	buf := NewAudioBuffer(format, time.Second)

	// Write enough data to exceed 200ms
	// 200ms at 24000 samples/sec * 2 bytes = 9600 bytes
	// So we need at least 5000 samples (10000 bytes)
	pcm1 := generatePCM(0.05, 3000)
	pcm2 := generatePCM(0.10, 3000)
	buf.Write(pcm1)
	buf.Write(pcm2)

	// Get recent data (should get what we have, up to 200ms)
	recent := buf.GetRecentData(200 * time.Millisecond)
	if recent == nil {
		t.Fatal("expected recent data, got nil")
	}

	expectedBytes := format.DurationToBytes(200 * time.Millisecond)
	// We wrote 12000 bytes total, so we should get the full 9600 bytes
	if len(recent) != expectedBytes {
		t.Errorf("expected %d bytes, got %d", expectedBytes, len(recent))
	}
}

func TestAudioBuffer_RingBufferWrap(t *testing.T) {
	format := DefaultAudioFormat()
	// Small buffer to force wrap-around
	buf := NewAudioBuffer(format, 100*time.Millisecond)

	// Write more data than buffer capacity to trigger wrap
	for i := 0; i < 10; i++ {
		pcm := generatePCM(float64(i+1)/10.0, 500)
		buf.Write(pcm)
	}

	// Buffer should maintain correct size
	maxBytes := format.DurationToBytes(100 * time.Millisecond)
	if buf.DataLength() > maxBytes {
		t.Errorf("expected DataLength <= %d, got %d", maxBytes, buf.DataLength())
	}
}

func TestAudioBuffer_AverageEnergy(t *testing.T) {
	format := DefaultAudioFormat()
	buf := NewAudioBuffer(format, time.Second)

	// Write a mix of energies
	low := generatePCM(0.05, 500)
	high := generatePCM(0.15, 500)

	buf.Write(low)
	time.Sleep(20 * time.Millisecond)
	buf.Write(high)
	time.Sleep(20 * time.Millisecond)
	buf.Write(low)
	time.Sleep(20 * time.Millisecond)
	buf.Write(high)

	avg := buf.AverageEnergy(200 * time.Millisecond)

	// Average should be somewhere between low and high
	if avg < 0.05 || avg > 0.15 {
		t.Errorf("expected average energy between 0.05 and 0.15, got %f", avg)
	}
}
