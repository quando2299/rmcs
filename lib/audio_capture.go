package main

import (
	"bufio"
	"encoding/binary"
	"io"
	"log"
	"os/exec"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	opus "gopkg.in/hraban/opus.v2"
)

// AudioCapture captures audio from microphone and sends to WebRTC track
type AudioCapture struct {
	track       *webrtc.TrackLocalStaticSample
	cmd         *exec.Cmd
	stdout      io.ReadCloser
	running     bool
	deviceInfo  *AudioDeviceInfo
	mu          sync.Mutex
	opusEncoder *opus.Encoder // Opus encoder
}

// NewAudioCapture creates audio capture instance
func NewAudioCapture(track *webrtc.TrackLocalStaticSample) *AudioCapture {
	deviceInfo := DetectAudioDevices()
	
	// 48000 Hz, 2 channels (stereo), VoIP application
	encoder, err := opus.NewEncoder(48000, 2, opus.AppVoIP)
	if err != nil {
		log.Printf("ERROR: Failed to create Opus encoder: %v", err)
		log.Println("Audio capture will not work without Opus encoder")
		return nil
	}
	
	// Set bitrate for VoIP (128 kbps)
	encoder.SetBitrate(128000)
	
	log.Println("Audio: Opus encoder initialized (48kHz stereo VoIP mode, 128kbps, 20ms frames)")
	
	return &AudioCapture{
		track:        track,
		running:      false,
		deviceInfo:   deviceInfo,
		opusEncoder:  encoder,
	}
}

// Start begins audio capture
func (a *AudioCapture) Start() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.running {
		return nil
	}

	// Unmute microphone and set volume BEFORE starting capture
	log.Println("Audio: Preparing microphone for capture...")
	if err := a.deviceInfo.UnmuteMicrophone(); err != nil {
		log.Printf("Warning: Microphone unmute failed, continuing anyway: %v", err)
	}
	if err := a.deviceInfo.SetMicrophoneVolume(); err != nil {
		log.Printf("Warning: Microphone volume setting failed, continuing anyway: %v", err)
	}

	// Start FFmpeg process directly
	var args []string
	
	if a.deviceInfo.UsePulseAudio {
		args = []string{
			"-f", "pulse",
			"-i", a.deviceInfo.InputDevice,
			"-af", "highpass=f=80,lowpass=f=8000,volume=3.5",
			"-f", "s16le",        // Signed 16-bit little-endian PCM
			"-ar", "48000",       // Resample 44.1kHz (AEC) -> 48kHz (Opus)
			"-ac", "2",           // Stereo (2 channels)
			"-vn",                // No vid
			"pipe:1",             // Output to stdout
		}
	} else {
		// ALSA fallback - Higher software boost (hardware only at 100%)
		args = []string{
			"-f", "alsa",
			"-i", a.deviceInfo.InputDevice,
			"-af", "highpass=f=80,lowpass=f=8000,acompressor=threshold=-20dB:ratio=3:attack=50:release=500,volume=4.0",
			"-f", "s16le",        // Signed 16-bit little-endian PCM
			"-ar", "48000",       // 48kHz (Opus requirement, no AEC in ALSA)
			"-ac", "2",           // Stereo (2 channels)
			"-vn",                // No vid
			"pipe:1",             // Output to stdout
		}
	}
	
	a.cmd = exec.Command("ffmpeg", args...)

	var err error
	a.stdout, err = a.cmd.StdoutPipe()
	if err != nil {
		return err
	}

	if err := a.cmd.Start(); err != nil {
		log.Printf("ERROR: Failed to start audio capture: %v", err)
		return err
	}

	a.running = true
	
	audioSystem := "PulseAudio"
	aecInfo := ""
	if a.deviceInfo.UsePulseAudio && a.deviceInfo.InputDevice == "echocancel_source" {
		aecInfo = " [AEC 44.1kHz -> FFmpeg resample 48kHz]"
	}
	if !a.deviceInfo.UsePulseAudio {
		audioSystem = "ALSA"
	}
	log.Printf("Audio capture started (%s%s, PCM 48kHz stereo → Opus)", audioSystem, aecInfo)

	// Start goroutine to read and send audio
	go a.captureLoop()

	return nil
}

// captureLoop reads PCM audio, encodes to Opus, and writes to WebRTC track
func (a *AudioCapture) captureLoop() {
	log.Println("Audio captureLoop started")
	
	// 48000 Hz × 2 channels × 2 bytes/sample × 0.020 sec = 3840 bytes per frame
	const pcmFrameSize = 3840
	const frameDuration = 20 * time.Millisecond
	const samplesPerFrame = 960  // 48000 Hz × 0.020 sec = 960 samples per channel
	
	// Create buffered reader for smooth reads
	reader := bufio.NewReaderSize(a.stdout, 16384)
	
	// Buffers
	pcmBuffer := make([]byte, pcmFrameSize)       // Input: 3840 bytes PCM
	pcmInt16 := make([]int16, samplesPerFrame*2)  // Convert to int16: 960 samples × 2 channels
	opusBuffer := make([]byte, 4000)              // Output: max Opus frame size
	
	packetCount := 0
	startTime := time.Now()
	
	for a.running {
		// Read exactly 3840 bytes (20ms of PCM audio)
		n, err := io.ReadFull(reader, pcmBuffer)
		if err != nil {
			if err == io.EOF {
				log.Println("Audio capture: EOF reached, stopping capture loop")
			} else if err == io.ErrUnexpectedEOF {
				log.Printf("Audio capture: Unexpected EOF (read %d/%d bytes), stopping capture loop", n, pcmFrameSize)
			} else {
				log.Printf("Audio capture read error: %v, stopping capture loop", err)
			}
			// Exit loop cleanly - restart mechanism removed (was causing infinite loop)
			break
		}
		
		// Verify we read the correct amount
		if n != pcmFrameSize {
			log.Printf("Warning: Read incomplete frame (%d/%d bytes), skipping", n, pcmFrameSize)
			continue
		}
		
		// Convert PCM bytes (little-endian int16) to int16 slice
		for i := 0; i < len(pcmInt16); i++ {
			pcmInt16[i] = int16(binary.LittleEndian.Uint16(pcmBuffer[i*2 : i*2+2]))
		}
		
		// Encode PCM to Opus
		// Encode(pcm []int16, data []byte) (int, error)
		// Returns number of bytes written to data buffer
		opusLen, err := a.opusEncoder.Encode(pcmInt16, opusBuffer)
		if err != nil {
			log.Printf("Opus encoding error: %v", err)
			continue
		}
		
		// Verify encoding produced reasonable output
		if opusLen <= 0 {
			log.Printf("Warning: Invalid Opus encoding (empty data)")
			continue
		}
		
		packetCount++
		
		// Log every 50 packets for diagnostics
		if packetCount % 50 == 0 {
			elapsed := time.Since(startTime).Seconds()
			packetsPerSec := float64(packetCount) / elapsed
			log.Printf("DEBUG: PCM -> Opus - Packets: %d, PCM: %d bytes, Opus: %d bytes, Rate: %.1f pkt/s", 
				packetCount, pcmFrameSize, opusLen, packetsPerSec)
		}
		
		// Log first successful encode
		if packetCount == 1 {
			log.Printf("PCM -> Opus: First frame encoded successfully (PCM: %d → Opus: %d bytes)", 
				pcmFrameSize, opusLen)
		}
		
		// Create WebRTC sample with encoded Opus data
		sample := media.Sample{
			Data:     opusBuffer[:opusLen],  // Encoded Opus data
			Duration: frameDuration,         // Fixed 20ms
		}
		
		// Write to WebRTC track
		if err := a.track.WriteSample(sample); err != nil {
			if a.running {
				log.Printf("Audio track write error: %v (packet %d, Opus size %d)", 
					err, packetCount, opusLen)
				continue
			}
			break
		}
	}
	
	log.Println("Audio capture loop stopped")
}

// Stop ends audio capture
func (a *AudioCapture) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.running {
		return
	}

	a.running = false

	if a.cmd != nil && a.cmd.Process != nil {
		a.cmd.Process.Kill()
		a.cmd.Wait()
	}

	if a.stdout != nil {
		a.stdout.Close()
	}

	log.Println("Audio capture stopped")
}
