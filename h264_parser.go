package main

import (
	"bytes"
)

// NAL unit types
const (
	NAL_UNIT_TYPE_SPS = 7
	NAL_UNIT_TYPE_PPS = 8
	NAL_UNIT_TYPE_IDR = 5
	NAL_UNIT_TYPE_NON_IDR = 1
)

// H264Parser helps parse H.264 NAL units from raw data
type H264Parser struct {
	sps []byte
	pps []byte
}

// FindNALUnits splits H.264 data into individual NAL units
// This handles MP4/MOV format with length-prefixed NAL units
func (p *H264Parser) FindNALUnits(data []byte) [][]byte {
	// Check if this is Annex-B format (with start codes) or MP4 format (length-prefixed)
	if p.isAnnexBFormat(data) {
		return p.parseAnnexBFormat(data)
	} else {
		return p.parseLengthPrefixedFormat(data)
	}
}

// Check if data uses Annex-B format (starts with 0x00000001 or 0x000001)
func (p *H264Parser) isAnnexBFormat(data []byte) bool {
	if len(data) < 4 {
		return false
	}

	// Check for 4-byte start code
	if data[0] == 0x00 && data[1] == 0x00 && data[2] == 0x00 && data[3] == 0x01 {
		return true
	}

	// Check for 3-byte start code
	if len(data) >= 3 && data[0] == 0x00 && data[1] == 0x00 && data[2] == 0x01 {
		return true
	}

	return false
}

// Parse MP4/MOV format with length-prefixed NAL units
func (p *H264Parser) parseLengthPrefixedFormat(data []byte) [][]byte {
	var nalUnits [][]byte

	i := 0
	for i < len(data) {
		// Need at least 4 bytes for length prefix
		if i+4 > len(data) {
			break
		}

		// Read 4-byte length (big endian)
		length := int(data[i])<<24 | int(data[i+1])<<16 | int(data[i+2])<<8 | int(data[i+3])
		i += 4

		// Check if we have enough data for this NAL unit
		if i+length > len(data) || length <= 0 {
			break
		}

		// Extract NAL unit
		nalUnit := make([]byte, length)
		copy(nalUnit, data[i:i+length])
		nalUnits = append(nalUnits, nalUnit)

		i += length
	}

	return nalUnits
}

// Parse Annex-B format with start codes
func (p *H264Parser) parseAnnexBFormat(data []byte) [][]byte {
	var nalUnits [][]byte

	// Look for NAL unit start codes (0x00000001 or 0x000001)
	for i := 0; i < len(data)-3; {
		// Check for 4-byte start code (0x00000001)
		if data[i] == 0x00 && data[i+1] == 0x00 && data[i+2] == 0x00 && data[i+3] == 0x01 {
			start := i + 4

			// Find next NAL unit or end of data
			nextStart := p.findNextStartCode(data, start)
			if nextStart == -1 {
				nextStart = len(data)
			}

			if nextStart > start {
				nalUnits = append(nalUnits, data[start:nextStart])
			}
			i = nextStart
		} else if data[i] == 0x00 && data[i+1] == 0x00 && data[i+2] == 0x01 {
			// Check for 3-byte start code (0x000001)
			start := i + 3

			// Find next NAL unit or end of data
			nextStart := p.findNextStartCode(data, start)
			if nextStart == -1 {
				nextStart = len(data)
			}

			if nextStart > start {
				nalUnits = append(nalUnits, data[start:nextStart])
			}
			i = nextStart
		} else {
			i++
		}
	}

	return nalUnits
}

func (p *H264Parser) findNextStartCode(data []byte, start int) int {
	for i := start; i < len(data)-3; i++ {
		if data[i] == 0x00 && data[i+1] == 0x00 && data[i+2] == 0x00 && data[i+3] == 0x01 {
			return i
		}
		if data[i] == 0x00 && data[i+1] == 0x00 && data[i+2] == 0x01 {
			return i
		}
	}
	return -1
}

// ProcessNALUnits processes NAL units and extracts SPS/PPS
func (p *H264Parser) ProcessNALUnits(nalUnits [][]byte) [][]byte {
	var processedUnits [][]byte

	for _, nalUnit := range nalUnits {
		if len(nalUnit) == 0 {
			continue
		}

		nalType := nalUnit[0] & 0x1F

		switch nalType {
		case NAL_UNIT_TYPE_SPS:
			p.sps = make([]byte, len(nalUnit))
			copy(p.sps, nalUnit)
			processedUnits = append(processedUnits, nalUnit)
		case NAL_UNIT_TYPE_PPS:
			p.pps = make([]byte, len(nalUnit))
			copy(p.pps, nalUnit)
			processedUnits = append(processedUnits, nalUnit)
		case NAL_UNIT_TYPE_IDR, NAL_UNIT_TYPE_NON_IDR:
			// For IDR frames, prepend SPS and PPS
			if nalType == NAL_UNIT_TYPE_IDR && len(p.sps) > 0 && len(p.pps) > 0 {
				processedUnits = append(processedUnits, p.sps)
				processedUnits = append(processedUnits, p.pps)
			}
			processedUnits = append(processedUnits, nalUnit)
		default:
			processedUnits = append(processedUnits, nalUnit)
		}
	}

	return processedUnits
}

// ConvertToAnnexB converts NAL units back to Annex B format with start codes
func (p *H264Parser) ConvertToAnnexB(nalUnits [][]byte) []byte {
	var buffer bytes.Buffer

	startCode := []byte{0x00, 0x00, 0x00, 0x01}

	for _, nalUnit := range nalUnits {
		buffer.Write(startCode)
		buffer.Write(nalUnit)
	}

	return buffer.Bytes()
}