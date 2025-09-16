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

	// 30 FPS = 33.33ms per frame
	ticker := time.NewTicker(time.Millisecond * 33)
	defer ticker.Stop()

	frameIndex := 0
	frameCounter := 0

	for {
		select {
		case <-v.stopChan:
			log.Println("Stopping video stream")
			v.isStreaming = false
			return
		case <-ticker.C:
			// Read the current frame file
			frameData, err := v.readH264File(v.frameFiles[frameIndex])
			if err != nil {
				log.Printf("Failed to read frame %d: %v", frameIndex, err)
				// Move to next frame even on error to maintain timing
				frameIndex = (frameIndex + 1) % len(v.frameFiles)
				frameCounter++
				continue
			}

			// Parse H.264 NAL units
			nalUnits := v.h264Parser.FindNALUnits(frameData)
			if len(nalUnits) == 0 {
				log.Printf("No NAL units found in frame %d", frameIndex)
				// Move to next frame even if no NAL units to maintain timing
				frameIndex = (frameIndex + 1) % len(v.frameFiles)
				frameCounter++
				continue
			}

			// Process NAL units (handle SPS/PPS)
			processedUnits := v.h264Parser.ProcessNALUnits(nalUnits)

			// Convert back to Annex B format
			processedData := v.h264Parser.ConvertToAnnexB(processedUnits)

			// Send the processed frame via WebRTC
			err = v.videoTrack.WriteSample(media.Sample{
				Data:     processedData,
				Duration: time.Millisecond * 33,
			})

			if err != nil {
				if err == io.ErrClosedPipe {
					log.Println("Track closed, stopping stream")
					v.isStreaming = false
					return
				}
				log.Printf("Failed to write frame %d: %v", frameIndex, err)
			}

			frameCounter++

			// Log progress every second (30 frames)
			if frameCounter%30 == 0 {
				elapsed := float64(frameCounter) / 30.0
				progress := float64(frameIndex) / float64(len(v.frameFiles)) * 100
				log.Printf("Streamed %d frames (%.1f seconds, %.1f%% through sequence, current: %s)",
					frameCounter, elapsed, progress, filepath.Base(v.frameFiles[frameIndex]))
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