package live

import (
	"encoding/binary"
	"math"
	"sync"
	"time"
)

// AudioFormat describes the PCM audio format.
type AudioFormat struct {
	SampleRate int // Samples per second (e.g., 24000)
	Channels   int // Number of channels (1 for mono)
	BitDepth   int // Bits per sample (16 for signed 16-bit)
}

// DefaultAudioFormat returns the standard format for live sessions.
func DefaultAudioFormat() AudioFormat {
	return AudioFormat{
		SampleRate: 24000,
		Channels:   1,
		BitDepth:   16,
	}
}

// BytesPerSample returns bytes per sample (bit depth / 8).
func (f AudioFormat) BytesPerSample() int {
	return f.BitDepth / 8
}

// BytesPerSecond returns bytes per second of audio.
func (f AudioFormat) BytesPerSecond() int {
	return f.SampleRate * f.Channels * f.BytesPerSample()
}

// DurationToBytes converts a duration to byte count.
func (f AudioFormat) DurationToBytes(d time.Duration) int {
	seconds := d.Seconds()
	return int(seconds * float64(f.BytesPerSecond()))
}

// BytesToDuration converts byte count to duration.
func (f AudioFormat) BytesToDuration(bytes int) time.Duration {
	seconds := float64(bytes) / float64(f.BytesPerSecond())
	return time.Duration(seconds * float64(time.Second))
}

// energySample stores energy measurement at a point in time.
type energySample struct {
	time   time.Time
	energy float64
}

// AudioBuffer accumulates PCM audio data and provides energy analysis.
// It's designed for efficient real-time processing in the VAD pipeline.
type AudioBuffer struct {
	mu sync.RWMutex

	format AudioFormat

	// Ring buffer for recent audio (for lookback)
	data       []byte
	writePos   int
	dataLen    int
	maxDataLen int

	// Energy history for sustained energy detection
	energyHistory []energySample
	maxHistory    time.Duration

	// Statistics
	totalBytesReceived int64
	lastWriteTime      time.Time
}

// NewAudioBuffer creates a new audio buffer with the given format.
// lookbackDuration specifies how much audio history to keep.
func NewAudioBuffer(format AudioFormat, lookbackDuration time.Duration) *AudioBuffer {
	maxBytes := format.DurationToBytes(lookbackDuration)
	if maxBytes < 1 {
		maxBytes = format.BytesPerSecond() // Default 1 second
	}

	return &AudioBuffer{
		format:        format,
		data:          make([]byte, maxBytes),
		maxDataLen:    maxBytes,
		energyHistory: make([]energySample, 0, 100),
		maxHistory:    lookbackDuration,
	}
}

// Write adds PCM audio data to the buffer.
func (b *AudioBuffer) Write(pcm []byte) {
	if len(pcm) == 0 {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	b.lastWriteTime = now
	b.totalBytesReceived += int64(len(pcm))

	// Calculate energy for this chunk
	energy := calculateRMSEnergy(pcm)
	b.energyHistory = append(b.energyHistory, energySample{
		time:   now,
		energy: energy,
	})

	// Prune old energy samples
	cutoff := now.Add(-b.maxHistory)
	newStart := 0
	for i, sample := range b.energyHistory {
		if sample.time.After(cutoff) {
			newStart = i
			break
		}
	}
	if newStart > 0 {
		b.energyHistory = b.energyHistory[newStart:]
	}

	// Write to ring buffer
	for _, byt := range pcm {
		b.data[b.writePos] = byt
		b.writePos = (b.writePos + 1) % b.maxDataLen
		if b.dataLen < b.maxDataLen {
			b.dataLen++
		}
	}
}

// CalculateEnergy calculates RMS energy of the given PCM chunk.
// Returns a value between 0.0 and 1.0.
func CalculateEnergy(pcm []byte) float64 {
	return calculateRMSEnergy(pcm)
}

// calculateRMSEnergy computes Root Mean Square energy for 16-bit PCM.
// Returns a value between 0.0 and 1.0.
func calculateRMSEnergy(pcm []byte) float64 {
	if len(pcm) < 2 {
		return 0
	}

	samples := len(pcm) / 2
	if samples == 0 {
		return 0
	}

	var sum float64
	for i := 0; i < len(pcm)-1; i += 2 {
		// Read 16-bit signed little-endian sample
		sample := int16(binary.LittleEndian.Uint16(pcm[i : i+2]))
		// Normalize to [-1.0, 1.0]
		normalized := float64(sample) / 32768.0
		sum += normalized * normalized
	}

	return math.Sqrt(sum / float64(samples))
}

// LastEnergy returns the most recent energy measurement.
func (b *AudioBuffer) LastEnergy() float64 {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if len(b.energyHistory) == 0 {
		return 0
	}
	return b.energyHistory[len(b.energyHistory)-1].energy
}

// AverageEnergy returns the average energy over the given duration.
func (b *AudioBuffer) AverageEnergy(duration time.Duration) float64 {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if len(b.energyHistory) == 0 {
		return 0
	}

	cutoff := time.Now().Add(-duration)
	var sum float64
	var count int

	for i := len(b.energyHistory) - 1; i >= 0; i-- {
		sample := b.energyHistory[i]
		if sample.time.Before(cutoff) {
			break
		}
		sum += sample.energy
		count++
	}

	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

// HasSustainedEnergy returns true if energy has been above threshold
// for at least the given duration.
func (b *AudioBuffer) HasSustainedEnergy(threshold float64, duration time.Duration) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if len(b.energyHistory) == 0 {
		return false
	}

	// Work backwards from most recent
	now := time.Now()
	cutoff := now.Add(-duration)
	startTime := now

	for i := len(b.energyHistory) - 1; i >= 0; i-- {
		sample := b.energyHistory[i]

		// If we've gone back far enough and all samples were above threshold
		if sample.time.Before(cutoff) {
			return true
		}

		// If energy drops below threshold, the sustained period breaks
		if sample.energy < threshold {
			return false
		}

		startTime = sample.time
	}

	// We ran out of history - check if the continuous period is long enough
	return now.Sub(startTime) >= duration
}

// IsSilent returns true if the most recent audio is below the threshold.
func (b *AudioBuffer) IsSilent(threshold float64) bool {
	return b.LastEnergy() < threshold
}

// SilenceDuration returns how long the audio has been below threshold.
// Returns 0 if currently above threshold.
func (b *AudioBuffer) SilenceDuration(threshold float64) time.Duration {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if len(b.energyHistory) == 0 {
		return 0
	}

	now := time.Now()

	// Walk backwards finding when silence started
	for i := len(b.energyHistory) - 1; i >= 0; i-- {
		sample := b.energyHistory[i]
		if sample.energy >= threshold {
			// Found non-silence, calculate duration since then
			if i == len(b.energyHistory)-1 {
				// Most recent sample is above threshold
				return 0
			}
			return now.Sub(b.energyHistory[i+1].time)
		}
	}

	// All history is silence
	if len(b.energyHistory) > 0 {
		return now.Sub(b.energyHistory[0].time)
	}
	return 0
}

// SpeechDuration returns how long the audio has been above threshold.
// Returns 0 if currently silent.
func (b *AudioBuffer) SpeechDuration(threshold float64) time.Duration {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if len(b.energyHistory) == 0 {
		return 0
	}

	now := time.Now()

	// Walk backwards finding when speech started
	for i := len(b.energyHistory) - 1; i >= 0; i-- {
		sample := b.energyHistory[i]
		if sample.energy < threshold {
			// Found silence, calculate duration since then
			if i == len(b.energyHistory)-1 {
				// Most recent sample is below threshold
				return 0
			}
			return now.Sub(b.energyHistory[i+1].time)
		}
	}

	// All history is speech
	if len(b.energyHistory) > 0 {
		return now.Sub(b.energyHistory[0].time)
	}
	return 0
}

// Reset clears the buffer.
func (b *AudioBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.writePos = 0
	b.dataLen = 0
	b.energyHistory = b.energyHistory[:0]
	b.totalBytesReceived = 0
}

// Format returns the audio format.
func (b *AudioBuffer) Format() AudioFormat {
	return b.format
}

// TotalBytesReceived returns the total bytes written to this buffer.
func (b *AudioBuffer) TotalBytesReceived() int64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.totalBytesReceived
}

// LastWriteTime returns when data was last written.
func (b *AudioBuffer) LastWriteTime() time.Time {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.lastWriteTime
}

// DataLength returns the current amount of buffered data in bytes.
func (b *AudioBuffer) DataLength() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.dataLen
}

// GetRecentData returns the most recent audio data up to the given duration.
// Returns a copy of the data.
func (b *AudioBuffer) GetRecentData(duration time.Duration) []byte {
	b.mu.RLock()
	defer b.mu.RUnlock()

	bytesNeeded := b.format.DurationToBytes(duration)
	if bytesNeeded > b.dataLen {
		bytesNeeded = b.dataLen
	}
	if bytesNeeded == 0 {
		return nil
	}

	result := make([]byte, bytesNeeded)

	// Calculate start position in ring buffer
	startPos := b.writePos - bytesNeeded
	if startPos < 0 {
		startPos += b.maxDataLen
	}

	// Copy data (handling wrap-around)
	if startPos+bytesNeeded <= b.maxDataLen {
		copy(result, b.data[startPos:startPos+bytesNeeded])
	} else {
		// Wrap around
		firstPart := b.maxDataLen - startPos
		copy(result[:firstPart], b.data[startPos:])
		copy(result[firstPart:], b.data[:bytesNeeded-firstPart])
	}

	return result
}
