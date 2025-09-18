package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

// NAL unit types based on C++ reference
const (
	NAL_SPS = 7  // Sequence Parameter Set
	NAL_PPS = 8  // Picture Parameter Set
	NAL_IDR = 5  // IDR frame
)

type VideoStreamer struct {
	track         *webrtc.TrackLocalStaticSample
	frameFiles    []string
	isStreaming   bool
	stopChan      chan bool
	mu            sync.Mutex

	// Cached NAL units like C++ implementation
	sps           []byte  // Type 7
	pps           []byte  // Type 8
	lastIDR       []byte  // Type 5

	// Timing management
	fps           uint32
	sampleDurationUs uint64  // microseconds per frame
	sampleTimeUs  uint64     // current sample timestamp in microseconds
	frameCounter  int
}

func NewVideoStreamer(track *webrtc.TrackLocalStaticSample) *VideoStreamer {
	fps := uint32(30)
	return &VideoStreamer{
		track:            track,
		stopChan:         make(chan bool),
		fps:              fps,
		sampleDurationUs: 1000000 / uint64(fps), // 33333 microseconds per frame at 30 FPS
		frameCounter:     -1,
	}
}

func (v *VideoStreamer) LoadH264Files(directory string) error {
	files, err := filepath.Glob(filepath.Join(directory, "*.h264"))
	if err != nil {
		return err
	}

	if len(files) == 0 {
		return fmt.Errorf("no H.264 files found in %s", directory)
	}

	// Sort files numerically like C++ implementation
	sort.Slice(files, func(i, j int) bool {
		numI := extractFileNumber(filepath.Base(files[i]))
		numJ := extractFileNumber(filepath.Base(files[j]))
		return numI < numJ
	})

	v.frameFiles = files
	log.Printf("Loaded %d H.264 files from %s", len(files), directory)

	// Parse first file to get initial NAL units
	if len(files) > 0 {
		v.parseInitialNALUnits(files[0])
	}

	return nil
}

func extractFileNumber(filename string) int {
	// Extract number from "sample-123.h264"
	parts := strings.Split(filename, "-")
	if len(parts) < 2 {
		return 0
	}
	numStr := strings.TrimSuffix(parts[1], ".h264")
	num, _ := strconv.Atoi(numStr)
	return num
}

func (v *VideoStreamer) parseInitialNALUnits(filepath string) error {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return err
	}

	// Parse NAL units with 4-byte length prefix
	i := 0
	for i < len(data) {
		if i+4 > len(data) {
			break
		}

		// Read 4-byte length (big-endian)
		length := binary.BigEndian.Uint32(data[i:i+4])
		naluStartIndex := i + 4
		naluEndIndex := naluStartIndex + int(length)

		if naluEndIndex > len(data) {
			break
		}

		// Get NAL unit type from header
		if naluStartIndex < len(data) {
			nalType := data[naluStartIndex] & 0x1F

			// Store ONLY the NAL unit data (without length prefix)
			nalUnitData := data[naluStartIndex:naluEndIndex]

			switch nalType {
			case NAL_SPS:
				v.sps = make([]byte, len(nalUnitData))
				copy(v.sps, nalUnitData)
				log.Printf("Cached SPS NAL unit (%d bytes)", len(v.sps))
			case NAL_PPS:
				v.pps = make([]byte, len(nalUnitData))
				copy(v.pps, nalUnitData)
				log.Printf("Cached PPS NAL unit (%d bytes)", len(v.pps))
			case NAL_IDR:
				v.lastIDR = make([]byte, len(nalUnitData))
				copy(v.lastIDR, nalUnitData)
				log.Printf("Cached IDR NAL unit (%d bytes)", len(v.lastIDR))
			}
		}

		i = naluEndIndex
	}

	return nil
}

func (v *VideoStreamer) getInitialNALUnits() []byte {
	// Return SPS + PPS + IDR in Annex B format for WebRTC
	var result []byte
	startCode := []byte{0x00, 0x00, 0x00, 0x01}

	if v.sps != nil {
		result = append(result, startCode...)
		result = append(result, v.sps...)
	}
	if v.pps != nil {
		result = append(result, startCode...)
		result = append(result, v.pps...)
	}
	if v.lastIDR != nil {
		result = append(result, startCode...)
		result = append(result, v.lastIDR...)
	}

	return result
}

func (v *VideoStreamer) StartStreaming() {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.isStreaming {
		return
	}

	v.isStreaming = true
	v.frameCounter = -1
	v.sampleTimeUs = 0

	go v.streamLoop()
}

func (v *VideoStreamer) StopStreaming() {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.isStreaming {
		v.stopChan <- true
		v.isStreaming = false
	}
}

func (v *VideoStreamer) streamLoop() {
	log.Println("Starting proper video stream with microsecond timing")

	// Send initial NAL units immediately
	if initialData := v.getInitialNALUnits(); len(initialData) > 0 {
		v.track.WriteSample(media.Sample{
			Data:     initialData,
			Duration: time.Duration(v.sampleDurationUs) * time.Microsecond,
		})
		log.Printf("Sent initial NAL units (%d bytes)", len(initialData))
	}

	// Create ticker with microsecond precision
	ticker := time.NewTicker(time.Duration(v.sampleDurationUs) * time.Microsecond)
	defer ticker.Stop()

	startTime := time.Now()
	framesSent := 0

	for {
		select {
		case <-v.stopChan:
			log.Printf("Stopping stream. Sent %d frames", framesSent)
			return

		case <-ticker.C:
			v.frameCounter++
			if v.frameCounter >= len(v.frameFiles) {
				if v.frameCounter > 0 {
					// Loop back to start
					v.frameCounter = 0
					log.Println("Looping video")
				}
			}

			// Read frame file
			filepath := v.frameFiles[v.frameCounter]
			data, err := os.ReadFile(filepath)
			if err != nil {
				log.Printf("Failed to read frame %d: %v", v.frameCounter, err)
				continue
			}

			// Convert to Annex B format for WebRTC
			annexBData := v.convertToAnnexB(data)

			// Update timing
			v.sampleTimeUs += v.sampleDurationUs

			// Send frame with proper duration
			err = v.track.WriteSample(media.Sample{
				Data:     annexBData,
				Duration: time.Duration(v.sampleDurationUs) * time.Microsecond,
			})

			if err != nil {
				if err == io.ErrClosedPipe {
					log.Println("Track closed")
					return
				}
				log.Printf("Write error: %v", err)
				continue
			}

			framesSent++

			// Log progress
			if framesSent%30 == 0 {
				elapsed := time.Since(startTime).Seconds()
				expectedTime := float64(framesSent) / float64(v.fps)
				drift := elapsed - expectedTime

				log.Printf("Sent %d frames | Elapsed: %.2fs | Expected: %.2fs | Drift: %.3fs | File: %s",
					framesSent, elapsed, expectedTime, drift, filepath)
			}
		}
	}
}

func (v *VideoStreamer) convertToAnnexB(data []byte) []byte {
	// Convert length-prefixed format to Annex B format for WebRTC
	var result []byte
	startCode := []byte{0x00, 0x00, 0x00, 0x01}

	i := 0
	for i < len(data) {
		if i+4 > len(data) {
			break
		}

		// Read 4-byte length (big-endian)
		length := binary.BigEndian.Uint32(data[i:i+4])
		naluStartIndex := i + 4
		naluEndIndex := naluStartIndex + int(length)

		if naluEndIndex > len(data) {
			break
		}

		// Append start code and NAL unit data
		result = append(result, startCode...)
		result = append(result, data[naluStartIndex:naluEndIndex]...)

		i = naluEndIndex
	}

	return result
}

func getCurrentTimeMicroseconds() uint64 {
	return uint64(time.Now().UnixNano() / 1000)
}