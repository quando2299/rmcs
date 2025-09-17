package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

type VideoStreamer struct {
	videoTrack *webrtc.TrackLocalStaticSample
	frameFiles []string
	isStreaming bool
	stopChan   chan bool
	h264Parser *H264Parser
}

func NewVideoStreamer(videoTrack *webrtc.TrackLocalStaticSample) *VideoStreamer {
	return &VideoStreamer{
		videoTrack: videoTrack,
		stopChan:   make(chan bool),
		h264Parser: &H264Parser{},
	}
}

func (v *VideoStreamer) LoadH264Files(directory string) error {
	files, err := filepath.Glob(filepath.Join(directory, "*.h264"))
	if err != nil {
		return err
	}

	// Sort files numerically
	sort.Slice(files, func(i, j int) bool {
		// Extract numbers from filenames
		numI := extractNumber(filepath.Base(files[i]))
		numJ := extractNumber(filepath.Base(files[j]))
		return numI < numJ
	})

	v.frameFiles = files
	duration := float64(len(files)) / 30.0
	log.Printf("Loaded %d H.264 files from %s (%.2f seconds at 30 FPS)", len(files), directory, duration)

	// Log first and last few files to verify order
	if len(files) > 0 {
		log.Printf("First file: %s", filepath.Base(files[0]))
		if len(files) > 1 {
			log.Printf("Last file: %s", filepath.Base(files[len(files)-1]))
		}
	}

	return nil
}

func extractNumber(filename string) int {
	// Extract number from filename like "sample-123.h264"
	parts := strings.Split(filename, "-")
	if len(parts) < 2 {
		return 0
	}
	numStr := strings.TrimSuffix(parts[1], ".h264")
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return 0
	}
	return num
}

func (v *VideoStreamer) StartStreaming() {
	if v.isStreaming {
		log.Println("Already streaming")
		return
	}

	if len(v.frameFiles) == 0 {
		log.Println("No H.264 files loaded")
		return
	}

	v.isStreaming = true
	go v.streamLoop()
}

func (v *VideoStreamer) streamLoop() {
	log.Println("Starting video stream at 30 FPS")

	// Pre-load ALL files into memory to eliminate I/O blocking
	log.Println("Pre-loading all H.264 files into memory...")
	frameDataCache := make([][]byte, len(v.frameFiles))
	for i, filePath := range v.frameFiles {
		data, err := v.readH264File(filePath)
		if err != nil {
			log.Printf("Failed to pre-load frame %d (%s): %v", i, filepath.Base(filePath), err)
			frameDataCache[i] = nil
			continue
		}
		frameDataCache[i] = data
	}
	log.Printf("Pre-loaded %d frames into memory", len(frameDataCache))

	// 30 FPS = 33.33ms per frame - use precise timing
	ticker := time.NewTicker(time.Duration(1000000000/30) * time.Nanosecond) // Exactly 30 FPS
	defer ticker.Stop()

	frameIndex := 0
	frameCounter := 0
	skippedFrames := 0

	for {
		select {
		case <-v.stopChan:
			log.Printf("Stopping video stream. Sent %d frames, skipped %d frames", frameCounter, skippedFrames)
			v.isStreaming = false
			return
		case <-ticker.C:
			// Use pre-loaded frame data
			frameData := frameDataCache[frameIndex]
			if frameData == nil {
				log.Printf("Skipping corrupted frame %d", frameIndex)
				frameIndex = (frameIndex + 1) % len(v.frameFiles)
				skippedFrames++
				continue
			}

			// Parse H.264 NAL units
			nalUnits := v.h264Parser.FindNALUnits(frameData)
			if len(nalUnits) == 0 {
				log.Printf("No NAL units in frame %d, skipping", frameIndex)
				frameIndex = (frameIndex + 1) % len(v.frameFiles)
				skippedFrames++
				continue
			}

			// Process NAL units (handle SPS/PPS)
			processedUnits := v.h264Parser.ProcessNALUnits(nalUnits)

			// Convert back to Annex B format
			processedData := v.h264Parser.ConvertToAnnexB(processedUnits)

			// Send the processed frame via WebRTC - non-blocking
			err := v.videoTrack.WriteSample(media.Sample{
				Data:     processedData,
				Duration: time.Duration(1000000000/30) * time.Nanosecond, // Exactly 33.333ms
			})

			if err != nil {
				if err == io.ErrClosedPipe {
					log.Println("Track closed, stopping stream")
					v.isStreaming = false
					return
				}
				log.Printf("Failed to write frame %d: %v", frameIndex, err)
				// Continue even on write error to maintain timing
			}

			frameCounter++

			// Log progress every second (30 frames)
			if frameCounter%30 == 0 {
				elapsed := float64(frameCounter) / 30.0
				totalFrames := len(v.frameFiles)
				cycleProgress := float64(frameIndex) / float64(totalFrames) * 100
				log.Printf("Streamed %d frames (%.1f sec elapsed, %.1f%% in current cycle, frame: %s)",
					frameCounter, elapsed, cycleProgress, filepath.Base(v.frameFiles[frameIndex]))
			}

			// Move to next frame, loop back to start if at end
			frameIndex = (frameIndex + 1) % len(v.frameFiles)
		}
	}
}

func (v *VideoStreamer) readH264File(filepath string) ([]byte, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %v", err)
	}

	return data, nil
}

func (v *VideoStreamer) StopStreaming() {
	if v.isStreaming {
		v.stopChan <- true
	}
}