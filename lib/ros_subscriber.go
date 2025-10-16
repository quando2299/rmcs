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

	// GStreamer stdin pipe for writing BGR images
	gstStdin  io.WriteCloser
	gstStdout io.ReadCloser

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

	// Timeout detection
	lastMessageTime time.Time
	timeoutDuration time.Duration

	// Benchmark tracking for encoding latency
	framesWritten     int
	framesRead        int
	lastBenchmarkTime time.Time

	// Frame rate limiting to prevent bursts
	lastFrameAcceptTime time.Time
	minFrameInterval    time.Duration
	framesDropped       int
}

func NewROSSubscriber(track *webrtc.TrackLocalStaticSample, cameraIndex int, rosMasterURI string) *ROSSubscriber {
	fps := uint32(30)

	// Map camera index to ROS topic name
	topicName := getTopicName(cameraIndex)

	return &ROSSubscriber{
		track:               track,
		topicName:           topicName,
		rosMasterURI:        rosMasterURI,
		stopChan:            make(chan bool),
		fps:                 fps,
		sampleDurationUs:    1000000 / uint64(fps),
		minFrameInterval:    time.Duration(1000000/uint64(fps)) * time.Microsecond, // 33.33ms at 30fps
		// Don't initialize dimensions - detect from first frame
		width:                0,
		height:               0,
		firstFrameReceived:   false,
		dimensionInitialized: false,
		timeoutDuration:      10 * time.Second, // 10 second timeout for no messages
		lastMessageTime:      time.Now(),
		lastBenchmarkTime:    time.Now(),
		lastFrameAcceptTime:  time.Time{},
	}
}

// CheckTopicExists verifies if a ROS topic is available
func CheckROSTopicExists(topicName string, rosMasterURI string) error {
	// Create temporary ROS node to check topic availability
	node, err := goroslib.NewNode(goroslib.NodeConf{
		Name:          "rmcs_topic_checker",
		MasterAddress: rosMasterURI,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to ROS master: %v", err)
	}
	defer node.Close()

	// Try to create a subscriber to verify topic exists
	// We don't need to actually receive messages, just verify it can subscribe
	sub, err := goroslib.NewSubscriber(goroslib.SubscriberConf{
		Node:  node,
		Topic: topicName,
		Callback: func(msg *sensor_msgs.Image) {
			// Empty callback - we just want to verify subscription works
		},
	})
	if err != nil {
		return fmt.Errorf("topic '%s' not available: %v", topicName, err)
	}
	defer sub.Close()

	// Give it a brief moment to establish subscription
	time.Sleep(100 * time.Millisecond)

	log.Printf("ROS topic '%s' is available", topicName)
	return nil
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
	r.lastMessageTime = time.Now()

	// Start timeout monitor goroutine
	go r.monitorTimeout()

	log.Printf("ROS subscriber started on topic: %s (waiting for first frame to detect dimensions)", r.topicName)
	return nil
}

// monitorTimeout checks if ROS messages have stopped arriving
func (r *ROSSubscriber) monitorTimeout() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopChan:
			return
		case <-ticker.C:
			r.mu.Lock()
			isRunning := r.isRunning
			lastTime := r.lastMessageTime
			timeout := r.timeoutDuration
			r.mu.Unlock()

			if !isRunning {
				return
			}

			timeSinceLastMsg := time.Since(lastTime)
			if timeSinceLastMsg > timeout {
				log.Printf("ROS_TIMEOUT_WARNING: No messages received on topic '%s' for %.1f seconds (last message: %v)",
					r.topicName, timeSinceLastMsg.Seconds(), lastTime.Format("15:04:05"))
			}
		}
	}
}

func (r *ROSSubscriber) initGStreamer() error {
	if r.width == 0 || r.height == 0 {
		return fmt.Errorf("cannot start GStreamer with zero dimensions")
	}

	log.Printf("Starting GStreamer with NVIDIA hardware encoder for dimensions: %dx%d", r.width, r.height)

	// GStreamer pipeline using NVIDIA hardware encoder
	// Use shell to properly handle the pipeline syntax
	pipeline := fmt.Sprintf(
		"gst-launch-1.0 -q fdsrc ! rawvideoparse width=%d height=%d format=bgr framerate=%d/1 ! "+
			"videoconvert ! nvvidconv ! "+
			"'video/x-raw(memory:NVMM),format=NV12' ! "+
			"nvv4l2h264enc maxperf-enable=1 bitrate=2000000 preset-level=1 idrinterval=%d control-rate=1 ! "+
			"h264parse config-interval=-1 ! fdsink",
		r.width, r.height, r.fps, r.fps,
	)

	r.cmd = exec.Command("/bin/sh", "-c", pipeline)

	// Log the exact command being executed for debugging
	log.Printf("GStreamer command: %s", pipeline)

	// Get stdin pipe for writing raw BGR frames
	gstStdin, err := r.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get GStreamer stdin: %v", err)
	}
	r.gstStdin = gstStdin

	// Get stdout pipe for reading H.264 stream
	stdout, err := r.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get GStreamer stdout: %v", err)
	}
	r.gstStdout = stdout

	// Get stderr pipe for logging
	stderr, err := r.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get GStreamer stderr: %v", err)
	}

	// Start GStreamer
	if err := r.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start GStreamer: %v", err)
	}

	// Log GStreamer stderr in background (errors and warnings only)
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			// Only log errors and warnings
			if len(line) > 0 && (line[0] == 'E' || line[0] == 'W') {
				log.Printf("[GStreamer ROS] %s", line)
			}
		}
	}()

	// Reset benchmark counters and frame limiting for new encoder instance
	// NOTE: Caller already holds r.mu lock, so don't lock again
	r.framesWritten = 0
	r.framesRead = 0
	r.framesDropped = 0
	r.lastBenchmarkTime = time.Now()
	r.lastFrameAcceptTime = time.Time{}

	// Start reading H.264 stream from GStreamer
	go r.readH264Stream(stdout)

	r.dimensionInitialized = true
	log.Printf("GStreamer NVIDIA hardware encoder started successfully for %dx%d @ %d fps", r.width, r.height, r.fps)
	return nil
}

func (r *ROSSubscriber) stopGStreamer() {
	if r.gstStdin != nil {
		r.gstStdin.Close()
		r.gstStdin = nil
	}

	if r.gstStdout != nil {
		r.gstStdout.Close()
		r.gstStdout = nil
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

	log.Println("GStreamer stopped")
}

func (r *ROSSubscriber) Stop() {
	// Check if already stopped and mark as stopping
	r.mu.Lock()
	if !r.isRunning {
		r.mu.Unlock()
		return
	}
	r.isRunning = false
	r.dimensionInitialized = false

	// Get references to things we need to clean up
	sub := r.sub
	node := r.node
	topicName := r.topicName
	r.sub = nil
	r.node = nil
	r.mu.Unlock()

	// Signal stop to readH264Stream goroutine
	select {
	case r.stopChan <- true:
	default:
	}

	// Close subscriber (might call callbacks, so done without holding lock)
	if sub != nil {
		sub.Close()
	}

	// Stop GStreamer
	r.stopGStreamer()

	// Close ROS node
	if node != nil {
		node.Close()
	}

	log.Printf("ROS subscriber stopped on topic: %s", topicName)
}

func (r *ROSSubscriber) handleImageMessage(msg *sensor_msgs.Image) {
	// Check if subscriber is still running
	r.mu.Lock()
	if !r.isRunning {
		r.mu.Unlock()
		return
	}

	// Update last message time for timeout monitoring
	r.lastMessageTime = time.Now()

	// Verify encoding is bgr8
	if msg.Encoding != "bgr8" {
		r.mu.Unlock()
		if r.messageCount == 0 {
			log.Printf("WARNING: unexpected encoding %s (expected bgr8)", msg.Encoding)
		}
		return
	}

	// First frame: detect dimensions and start GStreamer
	if !r.firstFrameReceived {
		r.firstFrameReceived = true
		r.width = msg.Width
		r.height = msg.Height
		log.Printf("Detected image dimensions from first frame: %dx%d", r.width, r.height)

		// Start GStreamer with detected dimensions
		if err := r.initGStreamer(); err != nil {
			r.mu.Unlock()
			log.Printf("ERROR: Failed to start GStreamer: %v", err)
			return
		}
	}

	// Handle dimension changes (restart GStreamer)
	if msg.Width != r.width || msg.Height != r.height {
		log.Printf("Image dimensions changed: %dx%d -> %dx%d. Restarting GStreamer...",
			r.width, r.height, msg.Width, msg.Height)

		// Stop old GStreamer
		r.stopGStreamer()

		// Update dimensions
		r.width = msg.Width
		r.height = msg.Height

		// Restart GStreamer with new dimensions
		if err := r.initGStreamer(); err != nil {
			r.mu.Unlock()
			log.Printf("ERROR: Failed to restart GStreamer: %v", err)
			return
		}
	}

	r.mu.Unlock()

	// Add logging to verify messages are being received
	r.messageCount++

	// Validate data size matches expected dimensions
	expectedSize := int(r.width * r.height * 3) // BGR8 = 3 bytes per pixel
	actualSize := len(msg.Data)
	if actualSize != expectedSize {
		log.Printf("ERROR: Data size mismatch. Expected %d bytes (%dx%dx3), got %d bytes",
			expectedSize, r.width, r.height, actualSize)
		log.Printf("ERROR: This will cause severe corruption. Skipping frame.")
		return
	}

	// Frame rate limiting: drop frames that arrive too fast
	r.mu.Lock()
	now := time.Now()
	timeSinceLastFrame := now.Sub(r.lastFrameAcceptTime)

	// Drop frame if it arrives too soon (maintaining max 30fps output)
	if !r.lastFrameAcceptTime.IsZero() && timeSinceLastFrame < r.minFrameInterval {
		r.framesDropped++
		r.mu.Unlock()
		return // Drop this frame
	}

	r.lastFrameAcceptTime = now
	gstStdin := r.gstStdin
	r.mu.Unlock()

	if gstStdin != nil {
		n, err := gstStdin.Write(msg.Data)
		if err != nil {
			// Only log if still running (avoid spam during shutdown)
			r.mu.Lock()
			stillRunning := r.isRunning
			r.mu.Unlock()
			if stillRunning {
				log.Printf("ERROR: Failed writing to GStreamer stdin: %v", err)
			}
			return
		}
		if n != len(msg.Data) {
			log.Printf("ERROR: Incomplete write to GStreamer. Expected %d bytes, wrote %d bytes", len(msg.Data), n)
			return
		}

		// Track frame written for benchmark
		r.mu.Lock()
		r.framesWritten++
		r.mu.Unlock()
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
			log.Printf("Stopping ROS stream.")
			return
		default:
			// Read data from GStreamer stdout
			n, err := reader.Read(readBuf)
			if err != nil {
				// EOF or pipe closed errors are expected during shutdown/switching
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

				case 8: // PPS
					r.mu.Lock()
					r.pps = make([]byte, len(nalUnit))
					copy(r.pps, nalUnit)
					r.mu.Unlock()

					// Send initial config when we have both SPS and PPS
					if waitingForConfig && r.sps != nil && r.pps != nil {
						waitingForConfig = false
						r.sendNALUnitNoSEI(r.sps)
						r.sendNALUnitNoSEI(r.pps)
					}

				case 5: // IDR
					r.mu.Lock()
					r.lastIDR = make([]byte, len(nalUnit))
					copy(r.lastIDR, nalUnit)
					r.mu.Unlock()
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
					framesSent++

					// Track frame read for benchmark
					r.mu.Lock()
					r.framesRead++
					r.mu.Unlock()

					// Report benchmark every 150 frames (5 seconds at 30fps)
					if framesSent%150 == 0 {
						r.reportBenchmark()
					}
				} else {
					r.sendNALUnitNoSEI(nalUnit)
				}
			}
		}
	}
}

func (r *ROSSubscriber) reportBenchmark() {
	r.mu.Lock()
	written := r.framesWritten
	read := r.framesRead
	dropped := r.framesDropped
	fps := r.fps
	now := time.Now()
	elapsed := now.Sub(r.lastBenchmarkTime).Seconds()
	r.lastBenchmarkTime = now
	r.framesDropped = 0 // Reset counter after reporting
	r.mu.Unlock()

	bufferedFrames := written - read
	latencyMs := (float64(bufferedFrames) / float64(fps)) * 1000.0

	var status string
	if latencyMs < 200 {
		status = "EXCELLENT"
	} else if latencyMs < 500 {
		status = "GOOD"
	} else if latencyMs < 800 {
		status = "OK"
	} else {
		status = "SLOW"
	}

	log.Printf("BENCHMARK [%s]: Latency: %.0fms | Buffer: %d frames | Rate: %.1f fps | Dropped: %d frames",
		status, latencyMs, bufferedFrames, 150.0/elapsed, dropped)
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
