package main

import (
	"bufio"
	"io"
	"log"
	"os/exec"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

type CameraCapture struct {
	track       *webrtc.TrackLocalStaticSample
	cmd         *exec.Cmd
	isRunning   bool
	stopChan    chan bool
	mu          sync.Mutex
	cameraIndex int

	// Cached NAL units
	sps     []byte
	pps     []byte
	lastIDR []byte

	// Timing
	fps              uint32
	sampleDurationUs uint64
}

func NewCameraCapture(track *webrtc.TrackLocalStaticSample, cameraIndex int) *CameraCapture {
	fps := uint32(30)
	return &CameraCapture{
		track:            track,
		cameraIndex:      cameraIndex,
		stopChan:         make(chan bool),
		fps:              fps,
		sampleDurationUs: 1000000 / uint64(fps),
	}
}

func (c *CameraCapture) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isRunning {
		return nil
	}

	// FFmpeg command to capture and encode
	c.cmd = exec.Command("ffmpeg",
		"-f", "avfoundation",
		"-framerate", "30",
		"-video_size", "640x480",
		"-i", "0", // Built-in camera
		"-c:v", "h264_videotoolbox", // Use hardware encoder
		"-profile:v", "baseline",
		"-level", "3.1",
		"-b:v", "1500k",
		"-maxrate", "1500k",
		"-bufsize", "3000k",
		"-g", "60",
		"-keyint_min", "30",
		"-pix_fmt", "yuv420p",
		"-bsf:v", "h264_mp4toannexb",
		"-f", "h264",
		"-",
	)

	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		return err
	}

	stderr, err := c.cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := c.cmd.Start(); err != nil {
		return err
	}

	c.isRunning = true

	// Log FFmpeg stderr in background
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Printf("[FFmpeg] %s", scanner.Text())
		}
	}()

	// Start reading H.264 stream
	go c.readH264Stream(stdout)

	log.Println("Camera capture started")
	return nil
}

func (c *CameraCapture) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.isRunning {
		return
	}

	c.stopChan <- true
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
	}
	c.isRunning = false
	log.Println("Camera capture stopped")
}

func (c *CameraCapture) readH264Stream(reader io.Reader) {
	buffer := make([]byte, 0, 100000)
	readBuf := make([]byte, 8192)
	framesSent := 0
	waitingForConfig := true

	for {
		select {
		case <-c.stopChan:
			log.Printf("Stopping camera stream. Sent %d frames", framesSent)
			return
		default:
			// Read data from FFmpeg stdout
			n, err := reader.Read(readBuf)
			if err != nil {
				if err == io.EOF {
					log.Println("Camera stream ended (EOF)")
					return
				}
				log.Printf("Error reading camera stream: %v", err)
				return
			}

			buffer = append(buffer, readBuf[:n]...)

			// Process NAL units from buffer
			for {
				nalUnit, remaining, found := c.extractNextNALUnit(buffer)
				if !found {
					buffer = remaining
					break
				}

				buffer = remaining

				if len(nalUnit) == 0 {
					continue
				}

				// Get NAL type
				nalType := nalUnit[0] & 0x1F

				// Cache configuration NAL units
				switch nalType {
				case 7: // SPS
					c.mu.Lock()
					c.sps = make([]byte, len(nalUnit))
					copy(c.sps, nalUnit)
					c.mu.Unlock()
					log.Printf("Cached SPS (%d bytes)", len(nalUnit))

				case 8: // PPS
					c.mu.Lock()
					c.pps = make([]byte, len(nalUnit))
					copy(c.pps, nalUnit)
					c.mu.Unlock()
					log.Printf("Cached PPS (%d bytes)", len(nalUnit))

					// Send initial config when we have both SPS and PPS
					if waitingForConfig && c.sps != nil && c.pps != nil {
						waitingForConfig = false
						log.Println("Sending initial SPS+PPS")
						c.sendNALUnit(c.sps)
						c.sendNALUnit(c.pps)
					}

				case 5: // IDR
					c.mu.Lock()
					c.lastIDR = make([]byte, len(nalUnit))
					copy(c.lastIDR, nalUnit)
					c.mu.Unlock()
				}

				// Skip frames until we have configuration
				if waitingForConfig {
					continue
				}

				// For IDR frames, prepend SPS+PPS
				if nalType == 5 {
					c.mu.Lock()
					if c.sps != nil {
						c.sendNALUnit(c.sps)
					}
					if c.pps != nil {
						c.sendNALUnit(c.pps)
					}
					c.mu.Unlock()
				}

				// Send the frame
				c.sendNALUnit(nalUnit)

				framesSent++
				if framesSent%90 == 0 {
					log.Printf("Sent %d frames from camera", framesSent)
				}
			}
		}
	}
}

func (c *CameraCapture) extractNextNALUnit(buffer []byte) (nalUnit []byte, remaining []byte, found bool) {
	// Find first start code
	startIdx := -1
	startCodeLen := 0

	for i := 0; i < len(buffer)-3; i++ {
		if buffer[i] == 0 && buffer[i+1] == 0 {
			if buffer[i+2] == 1 {
				startIdx = i
				startCodeLen = 3
				break
			}
			if i < len(buffer)-4 && buffer[i+2] == 0 && buffer[i+3] == 1 {
				startIdx = i
				startCodeLen = 4
				break
			}
		}
	}

	if startIdx == -1 {
		// No start code found, keep last 3 bytes
		if len(buffer) > 3 {
			return nil, buffer[len(buffer)-3:], false
		}
		return nil, buffer, false
	}

	// Find next start code
	nextIdx := -1
	for i := startIdx + startCodeLen; i < len(buffer)-3; i++ {
		if buffer[i] == 0 && buffer[i+1] == 0 {
			if buffer[i+2] == 1 {
				nextIdx = i
				break
			}
			if i < len(buffer)-4 && buffer[i+2] == 0 && buffer[i+3] == 1 {
				nextIdx = i
				break
			}
		}
	}

	if nextIdx == -1 {
		// No complete NAL unit yet
		return nil, buffer, false
	}

	// Extract NAL unit (without start code)
	nalUnit = buffer[startIdx+startCodeLen : nextIdx]
	remaining = buffer[nextIdx:]

	return nalUnit, remaining, true
}

func (c *CameraCapture) sendNALUnit(nalUnit []byte) {
	// Add Annex B start code
	startCode := []byte{0x00, 0x00, 0x00, 0x01}
	data := append(startCode, nalUnit...)

	err := c.track.WriteSample(media.Sample{
		Data:     data,
		Duration: time.Duration(c.sampleDurationUs) * time.Microsecond,
	})

	if err != nil && err != io.ErrClosedPipe {
		log.Printf("Error writing sample: %v", err)
	}
}

func (c *CameraCapture) GetInitialNALUnits() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()

	var result []byte
	startCode := []byte{0x00, 0x00, 0x00, 0x01}

	if c.sps != nil {
		result = append(result, startCode...)
		result = append(result, c.sps...)
	}
	if c.pps != nil {
		result = append(result, startCode...)
		result = append(result, c.pps...)
	}
	if c.lastIDR != nil {
		result = append(result, startCode...)
		result = append(result, c.lastIDR...)
	}

	return result
}