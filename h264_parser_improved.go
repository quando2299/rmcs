package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	NAL_TYPE_NON_IDR = 1  // Non-IDR slice
	NAL_TYPE_IDR     = 5  // IDR Frame
	NAL_TYPE_SEI     = 6  // Supplemental Enhancement Information
	NAL_TYPE_SPS     = 7  // Sequence Parameter Set
	NAL_TYPE_PPS     = 8  // Picture Parameter Set
	NAL_TYPE_AUD     = 9  // Access Unit Delimiter
)

type NALUnit struct {
	Type      uint8
	Data      []byte
	Timestamp time.Duration
}

type H264Sample struct {
	NALUnits  []NALUnit
	Timestamp time.Duration
	IsKeyFrame bool
}

type H264FileParser struct {
	files       []string
	currentFile int
	fps         float64
	frameDuration time.Duration
	frameNumber int64
	loop        bool

	// Store important NAL units for initialization
	sps *NALUnit
	pps *NALUnit
	idr *NALUnit
}

func NewH264FileParser(directory string, fps float64, loop bool) (*H264FileParser, error) {
	// Read all .h264 files from directory
	files, err := filepath.Glob(filepath.Join(directory, "*.h264"))
	if err != nil {
		return nil, fmt.Errorf("failed to read H.264 files: %w", err)
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no H.264 files found in directory: %s", directory)
	}

	// Sort files to ensure consistent order
	sort.Strings(files)

	log.Printf("Found %d H.264 files in %s", len(files), directory)

	return &H264FileParser{
		files:         files,
		currentFile:   0,
		fps:           fps,
		frameDuration: time.Duration(float64(time.Second) / fps),
		frameNumber:   0,
		loop:          loop,
	}, nil
}

// ParseNALUnits parses NAL units from H.264 file data
func (p *H264FileParser) ParseNALUnits(data []byte) ([]NALUnit, error) {
	var nalUnits []NALUnit
	i := 0

	for i < len(data) {
		// Check if we have enough data for length
		if i+4 > len(data) {
			break
		}

		// Read 4-byte length prefix (big-endian)
		length := binary.BigEndian.Uint32(data[i:i+4])
		i += 4

		// Check if we have enough data for the NAL unit
		if i+int(length) > len(data) {
			log.Printf("Warning: Incomplete NAL unit at position %d", i)
			break
		}

		// Extract NAL unit
		nalData := data[i:i+int(length)]
		i += int(length)

		if len(nalData) == 0 {
			continue
		}

		// Parse NAL header to get type
		nalType := nalData[0] & 0x1F

		nalUnit := NALUnit{
			Type: nalType,
			Data: nalData,
			Timestamp: p.frameDuration * time.Duration(p.frameNumber),
		}

		nalUnits = append(nalUnits, nalUnit)

		// Store important NAL units for stream initialization
		switch nalType {
		case NAL_TYPE_SPS:
			p.sps = &nalUnit
			log.Println("Found SPS NAL unit")
		case NAL_TYPE_PPS:
			p.pps = &nalUnit
			log.Println("Found PPS NAL unit")
		case NAL_TYPE_IDR:
			p.idr = &nalUnit
			log.Println("Found IDR frame")
		}
	}

	return nalUnits, nil
}

// GetInitialNALUnits returns SPS, PPS, and IDR for stream initialization
func (p *H264FileParser) GetInitialNALUnits() []NALUnit {
	var initial []NALUnit

	// Always send SPS first, then PPS, then IDR
	// This order is critical for H.264 decoders
	if p.sps != nil {
		// Create a fresh copy to avoid timing issues
		spsCopy := NALUnit{
			Type: p.sps.Type,
			Data: append([]byte(nil), p.sps.Data...),
			Timestamp: p.frameDuration * time.Duration(p.frameNumber),
		}
		initial = append(initial, spsCopy)
	}
	if p.pps != nil {
		ppsCopy := NALUnit{
			Type: p.pps.Type,
			Data: append([]byte(nil), p.pps.Data...),
			Timestamp: p.frameDuration * time.Duration(p.frameNumber),
		}
		initial = append(initial, ppsCopy)
	}
	if p.idr != nil {
		idrCopy := NALUnit{
			Type: p.idr.Type,
			Data: append([]byte(nil), p.idr.Data...),
			Timestamp: p.frameDuration * time.Duration(p.frameNumber),
		}
		initial = append(initial, idrCopy)
	}

	return initial
}

// NextSample reads and parses the next H.264 sample
func (p *H264FileParser) NextSample() (*H264Sample, error) {
	if p.currentFile >= len(p.files) {
		if p.loop {
			p.currentFile = 0
			// Don't reset frameNumber to maintain timing continuity
			log.Println("Looping video files")
		} else {
			return nil, io.EOF
		}
	}

	// Read current file
	data, err := os.ReadFile(p.files[p.currentFile])
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", p.files[p.currentFile], err)
	}

	// Parse NAL units
	nalUnits, err := p.ParseNALUnits(data)
	if err != nil {
		return nil, err
	}

	// Check if this is a keyframe (contains IDR)
	isKeyFrame := false
	for _, nal := range nalUnits {
		if nal.Type == NAL_TYPE_IDR {
			isKeyFrame = true
			break
		}
	}

	sample := &H264Sample{
		NALUnits:   nalUnits,
		Timestamp:  p.frameDuration * time.Duration(p.frameNumber),
		IsKeyFrame: isKeyFrame,
	}

	p.currentFile++
	p.frameNumber++

	return sample, nil
}

// ConvertNALUnitsToAnnexB converts NAL units to Annex B format for WebRTC
func ConvertNALUnitsToAnnexB(nalUnits []NALUnit) []byte {
	var result []byte
	startCode := []byte{0x00, 0x00, 0x00, 0x01}

	for _, nal := range nalUnits {
		result = append(result, startCode...)
		result = append(result, nal.Data...)
	}

	return result
}

// GetFrameDuration returns the duration of a single frame
func (p *H264FileParser) GetFrameDuration() time.Duration {
	return p.frameDuration
}

// Reset resets the parser to the beginning
func (p *H264FileParser) Reset() {
	p.currentFile = 0
	p.frameNumber = 0
}