package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os/exec"
	"sync"
	"time"

	"github.com/bluenviron/goroslib/v2"
	"github.com/bluenviron/goroslib/v2/pkg/msgs/sensor_msgs"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

type ROSSubscriber struct {
	track        *webrtc.TrackLocalStaticSample
	node         *goroslib.Node
	sub          *goroslib.Subscriber
	cmd          *exec.Cmd
	isRunning    bool
	stopChan     chan bool
	mu           sync.Mutex
	topicName    string
	rosMasterURI string

	// FFmpeg stdin pipe for writing BGR images
	ffmpegStdin  io.WriteCloser
	ffmpegStdout io.ReadCloser

	// Cached NAL units
	sps     []byte
	pps     []byte
	lastIDR []byte

	// Timing
	fps              uint32
	sampleDurationUs uint64

	// Image dimensions
	width  uint32
	height uint32

	// Message counter for logging
	messageCount int

	// First frame flag to detect dimensions
	firstFrameReceived   bool
	dimensionInitialized bool
}

func NewROSSubscriber(track *webrtc.TrackLocalStaticSample, cameraIndex int, rosMasterURI string) *ROSSubscriber {
	fps := uint32(30)

	// Map camera index to ROS topic name
	topicName := getTopicName(cameraIndex)

	return &ROSSubscriber{
		track:            track,
		topicName:        topicName,
		rosMasterURI:     rosMasterURI,
		stopChan:         make(chan bool),
		fps:              fps,
		sampleDurationUs: 1000000 / uint64(fps),
		// Don't initialize dimensions - detect from first frame
		width:                0,
		height:               0,
		firstFrameReceived:   false,
		dimensionInitialized: false,
	}
}

func getTopicName(cameraIndex int) string {
	switch cameraIndex {
	case 1:
		return "/leopard/id1/image_resized"
	case 2:
		return "/leopard/id2/image_resized"
	case 3:
		return "/leopard/id3/image_resized"
	case 4:
		return "/leopard/id4/image_resized"
	case 5:
		return "/leopard/id5/image_resized"
	case 6:
		return "/leopard/id6/image_resized"
	case 7:
		return "/leopard/id7/image_resized"
	case 8:
		return "/flir/id8/image_resized"
	default:
		return "/leopard/id1/image_resized"
	}
}

func (r *ROSSubscriber) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.isRunning {
		return nil
	}

	// Create ROS node
	node, err := goroslib.NewNode(goroslib.NodeConf{
		Name:          "rmcs_subscriber",
		MasterAddress: r.rosMasterURI,
	})
	if err != nil {
		return fmt.Errorf("failed to create ROS node: %v", err)
	}
	r.node = node

	// Create subscriber for ROS image topic FIRST to detect dimensions
	sub, err := goroslib.NewSubscriber(goroslib.SubscriberConf{
		Node:  r.node,
		Topic: r.topicName,
		Callback: func(msg *sensor_msgs.Image) {
			r.handleImageMessage(msg)
		},
	})
	if err != nil {
		r.node.Close()
		return fmt.Errorf("failed to create subscriber: %v", err)
	}
	r.sub = sub

	r.isRunning = true
	log.Printf("ROS subscriber started on topic: %s (waiting for first frame to detect dimensions)", r.topicName)
	return nil
}

func (r *ROSSubscriber) initFFmpeg() error {
	if r.width == 0 || r.height == 0 {
		return fmt.Errorf("cannot start FFmpeg with zero dimensions")
	}

	log.Printf("Starting FFmpeg with dimensions: %dx%d", r.width, r.height)

	args := []string{
		"-f", "rawvideo",
		"-pixel_format", "bgr24", // ROS bgr8 = 3 bytes per pixel (8 bits per channel)
		"-video_size", fmt.Sprintf("%dx%d", r.width, r.height),
		"-framerate", fmt.Sprintf("%d", r.fps),
		"-i", "pipe:0", // Read from stdin
		"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2", // Ensure even dimensions
		"-c:v", "libx264", // H264 codec
		"-preset", "veryfast", // Faster encoding for better framerate
		"-crf", "28", // Lower quality for faster encoding
		"-g", "60", // Keyframe every 2 seconds
		"-bf", "0", // No B-frames for lower latency
		"-refs", "1", // Single reference frame for speed
		"-threads", "0", // Auto-detect thread count
		"-pix_fmt", "yuv420p",
		"-r", fmt.Sprintf("%d", r.fps), // Output framerate
		"-bsf:v", "h264_mp4toannexb", // Convert to Annex B format
		"-f", "h264", // Raw H264 output
		"-",
	}

	r.cmd = exec.Command("ffmpeg", args...)

	// Get stdin pipe for writing raw BGR frames
	ffmpegStdin, err := r.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get FFmpeg stdin: %v", err)
	}
	r.ffmpegStdin = ffmpegStdin

	// Get stdout pipe for reading H.264 stream
	stdout, err := r.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get FFmpeg stdout: %v", err)
	}
	r.ffmpegStdout = stdout

	// Get stderr pipe for logging
	stderr, err := r.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get FFmpeg stderr: %v", err)
	}

	// Start FFmpeg
	if err := r.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start FFmpeg: %v", err)
	}

	// Log FFmpeg stderr in background
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Printf("[FFmpeg ROS] %s", scanner.Text())
		}
	}()

	// Start reading H.264 stream from FFmpeg
	go r.readH264Stream(stdout)

	r.dimensionInitialized = true
	log.Printf("FFmpeg started successfully for %dx%d @ %d fps", r.width, r.height, r.fps)
	return nil
}

func (r *ROSSubscriber) stopFFmpeg() {
	if r.ffmpegStdin != nil {
		r.ffmpegStdin.Close()
		r.ffmpegStdin = nil
	}

	if r.ffmpegStdout != nil {
		r.ffmpegStdout.Close()
		r.ffmpegStdout = nil
	}

	if r.cmd != nil && r.cmd.Process != nil {
		r.cmd.Process.Kill()
		r.cmd.Wait() // Wait for process to exit
		r.cmd = nil
	}

	// Clear cached NAL units
	r.sps = nil
	r.pps = nil
	r.lastIDR = nil

	log.Println("FFmpeg stopped")
}

func (r *ROSSubscriber) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.isRunning {
		return
	}

	// Signal stop to readH264Stream goroutine
	select {
	case r.stopChan <- true:
	default:
	}

	if r.sub != nil {
		r.sub.Close()
		r.sub = nil
	}

	r.stopFFmpeg()

	if r.node != nil {
		r.node.Close()
		r.node = nil
	}

	r.isRunning = false
	r.dimensionInitialized = false
	log.Printf("ROS subscriber stopped on topic: %s", r.topicName)
}

func (r *ROSSubscriber) handleImageMessage(msg *sensor_msgs.Image) {
	// Check if subscriber is still running
	r.mu.Lock()
	if !r.isRunning {
		r.mu.Unlock()
		return
	}

	// Verify encoding is bgr8
	if msg.Encoding != "bgr8" {
		r.mu.Unlock()
		if r.messageCount == 0 {
			log.Printf("WARNING: unexpected encoding %s (expected bgr8)", msg.Encoding)
		}
		return
	}

	// First frame: detect dimensions and start FFmpeg
	if !r.firstFrameReceived {
		r.firstFrameReceived = true
		r.width = msg.Width
		r.height = msg.Height
		log.Printf("Detected image dimensions from first frame: %dx%d", r.width, r.height)

		// Start FFmpeg with detected dimensions
		if err := r.initFFmpeg(); err != nil {
			r.mu.Unlock()
			log.Printf("ERROR: Failed to start FFmpeg: %v", err)
			return
		}
	}

	// Handle dimension changes (restart FFmpeg)
	if msg.Width != r.width || msg.Height != r.height {
		log.Printf("Image dimensions changed: %dx%d -> %dx%d. Restarting FFmpeg...",
			r.width, r.height, msg.Width, msg.Height)

		// Stop old FFmpeg
		r.stopFFmpeg()

		// Update dimensions
		r.width = msg.Width
		r.height = msg.Height

		// Restart FFmpeg with new dimensions
		if err := r.initFFmpeg(); err != nil {
			r.mu.Unlock()
			log.Printf("ERROR: Failed to restart FFmpeg: %v", err)
			return
		}
	}

	r.mu.Unlock()

	// Add logging to verify messages are being received
	r.messageCount++
	if r.messageCount%30 == 1 {
		log.Printf("Received ROS image message %d: %dx%d, encoding=%s, data_len=%d",
			r.messageCount, msg.Width, msg.Height, msg.Encoding, len(msg.Data))
	}

	// Validate data size matches expected dimensions
	expectedSize := int(r.width * r.height * 3) // BGR8 = 3 bytes per pixel
	actualSize := len(msg.Data)
	if actualSize != expectedSize {
		log.Printf("ERROR: Data size mismatch. Expected %d bytes (%dx%dx3), got %d bytes",
			expectedSize, r.width, r.height, actualSize)
		log.Printf("ERROR: This will cause severe corruption. Skipping frame.")
		return
	}

	// Write raw BGR data to FFmpeg stdin
	r.mu.Lock()
	ffmpegStdin := r.ffmpegStdin
	r.mu.Unlock()

	if ffmpegStdin != nil {
		n, err := ffmpegStdin.Write(msg.Data)
		if err != nil {
			// Only log if still running (avoid spam during shutdown)
			r.mu.Lock()
			stillRunning := r.isRunning
			r.mu.Unlock()
			if stillRunning {
				log.Printf("ERROR: Failed writing to FFmpeg stdin: %v", err)
			}
			return
		}
		if n != len(msg.Data) {
			log.Printf("ERROR: Incomplete write to FFmpeg. Expected %d bytes, wrote %d bytes", len(msg.Data), n)
			return
		}
		if r.messageCount <= 3 {
			log.Printf("Wrote %d bytes to FFmpeg stdin (frame %d)", n, r.messageCount)
		}
	}
}

func (r *ROSSubscriber) readH264Stream(reader io.Reader) {
	buffer := make([]byte, 0, 100000)
	readBuf := make([]byte, 8192)
	framesSent := 0
	waitingForConfig := true

	for {
		select {
		case <-r.stopChan:
			log.Printf("Stopping ROS stream. Sent %d frames", framesSent)
			return
		default:
			// Read data from FFmpeg stdout
			n, err := reader.Read(readBuf)
			if err != nil {
				if err == io.EOF {
					log.Println("ROS stream ended (EOF)")
					return
				}
				log.Printf("Error reading ROS stream: %v", err)
				return
			}

			buffer = append(buffer, readBuf[:n]...)

			// Process NAL units from buffer
			for {
				nalUnit, remaining, found := r.extractNextNALUnit(buffer)
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
					r.mu.Lock()
					r.sps = make([]byte, len(nalUnit))
					copy(r.sps, nalUnit)
					r.mu.Unlock()
					log.Printf("ROS: Cached SPS (%d bytes)", len(nalUnit))

				case 8: // PPS
					r.mu.Lock()
					r.pps = make([]byte, len(nalUnit))
					copy(r.pps, nalUnit)
					r.mu.Unlock()
					log.Printf("ROS: Cached PPS (%d bytes)", len(nalUnit))

					// Send initial config when we have both SPS and PPS
					if waitingForConfig && r.sps != nil && r.pps != nil {
						waitingForConfig = false
						log.Println("ROS: Sending initial SPS+PPS")
						r.sendNALUnitNoSEI(r.sps)
						r.sendNALUnitNoSEI(r.pps)
					}

				case 5: // IDR
					r.mu.Lock()
					r.lastIDR = make([]byte, len(nalUnit))
					copy(r.lastIDR, nalUnit)
					r.mu.Unlock()
					if framesSent <= 3 {
						log.Printf("ROS: Cached IDR frame (%d bytes)", len(nalUnit))
					}
				}

				// Skip frames until we have configuration
				if waitingForConfig {
					continue
				}

				// For IDR frames, prepend SPS+PPS (without SEI)
				if nalType == 5 {
					r.mu.Lock()
					if r.sps != nil {
						r.sendNALUnitNoSEI(r.sps)
					}
					if r.pps != nil {
						r.sendNALUnitNoSEI(r.pps)
					}
					r.mu.Unlock()
				}

				// Send the frame WITH SEI only for video slices (types 1, 5)
				if nalType == 1 || nalType == 5 {
					r.sendNALUnitWithSEI(nalUnit)
				} else {
					r.sendNALUnitNoSEI(nalUnit)
				}

				framesSent++
				if framesSent%90 == 0 {
					log.Printf("Sent %d frames from ROS topic %s", framesSent, r.topicName)
				}
			}
		}
	}
}

func (r *ROSSubscriber) extractNextNALUnit(buffer []byte) (nalUnit []byte, remaining []byte, found bool) {
	// Need at least 4 bytes to check for start code
	if len(buffer) < 4 {
		return nil, buffer, false
	}

	// Find first start code
	startIdx := -1
	startCodeLen := 0

	for i := 0; i <= len(buffer)-3; i++ {
		if buffer[i] == 0 && buffer[i+1] == 0 {
			if buffer[i+2] == 1 {
				startIdx = i
				startCodeLen = 3
				break
			}
			if i <= len(buffer)-4 && buffer[i+2] == 0 && buffer[i+3] == 1 {
				startIdx = i
				startCodeLen = 4
				break
			}
		}
	}

	if startIdx == -1 {
		// No start code found, keep last 3 bytes for next read
		if len(buffer) > 3 {
			return nil, buffer[len(buffer)-3:], false
		}
		return nil, buffer, false
	}

	// Find next start code
	nextIdx := -1
	for i := startIdx + startCodeLen; i <= len(buffer)-3; i++ {
		if buffer[i] == 0 && buffer[i+1] == 0 {
			if buffer[i+2] == 1 {
				nextIdx = i
				break
			}
			if i <= len(buffer)-4 && buffer[i+2] == 0 && buffer[i+3] == 1 {
				nextIdx = i
				break
			}
		}
	}

	if nextIdx == -1 {
		// No complete NAL unit yet, need more data
		return nil, buffer, false
	}

	// Extract NAL unit (without start code)
	nalUnit = buffer[startIdx+startCodeLen : nextIdx]
	remaining = buffer[nextIdx:]

	return nalUnit, remaining, true
}

func (r *ROSSubscriber) sendNALUnitWithSEI(nalUnit []byte) {
	startCode := []byte{0x00, 0x00, 0x00, 0x01}

	// Get current timestamp in microseconds
	timestampUs := uint64(time.Now().UnixNano() / 1000)

	// Create SEI with timestamp
	seiNAL := createSimpleTimestampSEI(timestampUs)

	// Build frame: SEI + NAL unit
	var data []byte
	data = append(data, startCode...)
	data = append(data, seiNAL...)
	data = append(data, startCode...)
	data = append(data, nalUnit...)

	err := r.track.WriteSample(media.Sample{
		Data:     data,
		Duration: time.Duration(r.sampleDurationUs) * time.Microsecond,
	})

	if err != nil && err != io.ErrClosedPipe {
		log.Printf("Error writing sample: %v", err)
	}
}

func (r *ROSSubscriber) sendNALUnitNoSEI(nalUnit []byte) {
	startCode := []byte{0x00, 0x00, 0x00, 0x01}

	// Build frame: just NAL unit without SEI
	var data []byte
	data = append(data, startCode...)
	data = append(data, nalUnit...)

	err := r.track.WriteSample(media.Sample{
		Data:     data,
		Duration: time.Duration(r.sampleDurationUs) * time.Microsecond,
	})

	if err != nil && err != io.ErrClosedPipe {
		log.Printf("Error writing sample: %v", err)
	}
}

func (r *ROSSubscriber) GetInitialNALUnits() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()

	var result []byte
	startCode := []byte{0x00, 0x00, 0x00, 0x01}

	if r.sps != nil {
		result = append(result, startCode...)
		result = append(result, r.sps...)
	}
	if r.pps != nil {
		result = append(result, startCode...)
		result = append(result, r.pps...)
	}
	if r.lastIDR != nil {
		result = append(result, startCode...)
		result = append(result, r.lastIDR...)
	}

	return result
}

// Note: SEI timestamp functions are already defined in camera_capture.go
// We can reuse those functions since they're in the same package
