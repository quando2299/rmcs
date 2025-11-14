package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/pion/webrtc/v4"
)

// TrackInfo holds information about a video track and its associated ROS subscriber
type TrackInfo struct {
	track         *webrtc.TrackLocalStaticSample
	rosSubscriber *ROSSubscriber
	cameraNumber  int
}

type WebRTCManager struct {
	peerConnections map[string]*webrtc.PeerConnection
	tracks          map[string]*TrackInfo // trackID -> TrackInfo (e.g., "left", "right")
	peerToTrack     map[string]string     // peerID -> trackID
	videoTrack      *webrtc.TrackLocalStaticSample
	audioTrack      *webrtc.TrackLocalStaticSample // Audio track for bidirectional communication - vnextthongnv
	audioCapture    *AudioCapture                  // Microphone capture - vnextthongnv
	audioPlayback   *AudioPlayback                 // Speaker playback - vnextthongnv
	videoStreamer   *VideoStreamer
	cameraCapture   *CameraCapture
	rosSubscriber   *ROSSubscriber // Deprecated: kept for backward compatibility
	useROSMode      bool
	useCameraMode   bool
	rosMasterURI    string
	customTopics    map[int]string // Map camera number (1-7) to custom topic names
	mu              sync.Mutex
}

// ICECandidateMessage represents an ICE candidate from Flutter
type ICECandidateMessage struct {
	Candidate     string `json:"candidate"`
	SDPMid        string `json:"sdpMid"`
	SDPMLineIndex uint16 `json:"sdpMLineIndex"`
}

// extractTrackID extracts the track identifier from peer ID
// Each unique peerID gets its own track for multi-camera support
// Returns: the peerID itself as trackID (one track per peer)
func extractTrackID(peerID string) string {
	// Use peerID directly as trackID
	// This allows unlimited simultaneous camera streams
	// Example: peerID "1762312010698752" → trackID "1762312010698752"
	return peerID
}

func NewWebRTCManager() (*WebRTCManager, error) {
	// We'll create peer connections on demand now

	// Create a video track for H264 with proper codec parameters
	videoTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeH264,
			ClockRate:   90000,
			Channels:    0,
			SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42001f",
		},
		"video",
		"stream",
	)
	if err != nil {
		return nil, err
	}
		
	// Create an audio track for Opus with VoIP optimization - vnextthongnv
	audioTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeOpus,
			ClockRate:   48000,
			Channels:    2,
			SDPFmtpLine: "minptime=10;useinbandfec=1",
		},
		"audio",
		"stream",
	)
	if err != nil {
		return nil, err
	}
	log.Println("Audio track created successfully (Opus 48kHz stereo)") // vnextthongnv

	// Use ROS mode for streaming from ROS topics
	useROSMode := true

	// Get ROS Master URI from environment variable, default to Docker network
	rosMasterURI := os.Getenv("ROS_MASTER_URI")
	if rosMasterURI == "" {
		rosMasterURI = "ros-master:11311" // Default for Docker network
	} else {
		// Remove http:// prefix if present (goroslib doesn't need it)
		rosMasterURI = strings.TrimPrefix(rosMasterURI, "http://")
	}
	log.Printf("Using ROS Master URI: %s", rosMasterURI)

	var rosSubscriber *ROSSubscriber

	// Load custom topics from environment variables (ROS_TOPIC_1 through ROS_TOPIC_7)
	customTopics := make(map[int]string)
	for i := 1; i <= 7; i++ {
		envVar := fmt.Sprintf("ROS_TOPIC_%d", i)
		if topic := os.Getenv(envVar); topic != "" {
			customTopics[i] = topic
			log.Printf("Loaded custom topic for camera %d: %s", i, topic)
		}
	}

	// Start with camera 1
	cameraIndex := 1

	// Create ROS subscriber (legacy single-track mode)
	rosSubscriber = NewROSSubscriber(videoTrack, cameraIndex, rosMasterURI, "legacy")

	// Override topic name if custom topic is specified for camera 1
	if customTopic, exists := customTopics[1]; exists {
		rosSubscriber.topicName = customTopic
		log.Printf("ROS mode enabled - will subscribe to custom topic: %s", customTopic)
	} else {
		log.Printf("ROS mode enabled - will subscribe to topic for camera %d: %s", cameraIndex, rosSubscriber.topicName)
	}

	// Create audio capture and playback - vnextthongnv
	audioCapture := NewAudioCapture(audioTrack)
	audioPlayback := NewAudioPlayback()

	return &WebRTCManager{
		peerConnections: make(map[string]*webrtc.PeerConnection),
		tracks:          make(map[string]*TrackInfo),
		peerToTrack:     make(map[string]string),
		videoTrack:      videoTrack,
		audioTrack:      audioTrack,   // vnextthongnv
		audioCapture:    audioCapture, // vnextthongnv
		audioPlayback:   audioPlayback, // vnextthongnv
		videoStreamer:   nil, // No file-based streaming
		cameraCapture:   nil, // No direct camera capture
		rosSubscriber:   rosSubscriber,
		useROSMode:      useROSMode,
		useCameraMode:   false,
		rosMasterURI:    rosMasterURI,
		customTopics:    customTopics,
	}, nil
}

// getOrCreateTrack gets an existing track or creates a new one for the given trackID
// Caller must hold w.mu lock
func (w *WebRTCManager) getOrCreateTrack(trackID string) (*TrackInfo, error) {
	// Check if track already exists
	if trackInfo, exists := w.tracks[trackID]; exists {
		return trackInfo, nil
	}

	log.Printf("Creating new track for trackID: %s", trackID)

	// Create new video track
	videoTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeH264,
			ClockRate:   90000,
			Channels:    0,
			SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42001f",
		},
		fmt.Sprintf("video-%s", trackID),
		fmt.Sprintf("stream-%s", trackID),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create video track: %v", err)
	}

	// Create ROS subscriber for this track (start with camera 1)
	var rosSubscriber *ROSSubscriber
	if w.useROSMode {
		cameraNumber := 1 // Default camera
		rosSubscriber = NewROSSubscriber(videoTrack, cameraNumber, w.rosMasterURI, trackID)

		// Override topic if custom topic is specified
		if customTopic, exists := w.customTopics[cameraNumber]; exists {
			rosSubscriber.topicName = customTopic
			log.Printf("Track %s: using custom topic for camera %d: %s", trackID, cameraNumber, customTopic)
		}
	}

	trackInfo := &TrackInfo{
		track:         videoTrack,
		rosSubscriber: rosSubscriber,
		cameraNumber:  1, // Start with camera 1
	}

	w.tracks[trackID] = trackInfo
	log.Printf("Created new track: %s with camera %d", trackID, trackInfo.cameraNumber)

	return trackInfo, nil
}

func (w *WebRTCManager) ProcessOffer(peerID string, offerSDP string) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Extract track ID from peer ID
	trackID := extractTrackID(peerID)
	log.Printf("Processing offer for peer %s, trackID: %s", peerID, trackID)

	// Get or create track for this trackID
	trackInfo, err := w.getOrCreateTrack(trackID)
	if err != nil {
		return "", fmt.Errorf("failed to get/create track: %v", err)
	}

	// Map peer to track
	w.peerToTrack[peerID] = trackID

	// Close existing connection if any
	if existingPC, exists := w.peerConnections[peerID]; exists {
		log.Printf("Closing existing peer connection for %s", peerID)
		existingPC.Close()
	}

	// Create new peer connection
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	peerConnection, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return "", err
	}

	// Add the specific video track for this peer
	_, err = peerConnection.AddTrack(trackInfo.track)
	if err != nil {
		peerConnection.Close()
		return "", err
	}
	log.Printf("Added track %s to peer %s", trackID, peerID)

	// Add the audio track to the new peer connection - vnextthongnv
	_, err = peerConnection.AddTrack(w.audioTrack)
	if err != nil {
		peerConnection.Close()
		return "", err
	}
	log.Printf("[%s] Added video and audio tracks to peer connection", peerID) // vnextthongnv

	// Set up OnTrack handler to receive remote audio from browser - vnextthongnv
	peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		if track.Kind() == webrtc.RTPCodecTypeAudio {
			log.Printf("[%s] Remote AUDIO track received from browser (browser mic -> VM speaker)", peerID)
			log.Printf("[%s] Audio codec: %s, PayloadType: %d, ClockRate: %d",
				peerID, track.Codec().MimeType, track.PayloadType(), track.Codec().ClockRate)

			// Start the playback loop to decode and play the audio
			go w.audioPlayback.PlaybackLoop(track)
			log.Printf("[%s] Audio playback loop started for remote track", peerID)
		} else {
			log.Printf("[%s] Ignoring non-audio track: %s", peerID, track.Kind().String())
		}
	})

	// Set up connection state handlers
	peerConnection.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		log.Printf("[%s] ICE connection state changed: %s", peerID, state.String())
	})

	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("[%s] WebRTC connection state changed: %s", peerID, state.String())

		switch state {
		case webrtc.PeerConnectionStateConnected:
			log.Printf("[%s] WebRTC connected, starting video stream", peerID)

			// Get track for this peer (multi-track architecture)
			w.mu.Lock()
			trackID := w.peerToTrack[peerID]
			trackInfo, exists := w.tracks[trackID]
			w.mu.Unlock()

			// Start audio capture/playback on first peer connection (audio is shared)
			// Check if any peer was already connected before starting audio
			w.mu.Lock()
			alreadyHadConnection := false
			for otherPeerID, pc := range w.peerConnections {
				if otherPeerID != peerID && pc.ConnectionState() == webrtc.PeerConnectionStateConnected {
					alreadyHadConnection = true
					break
				}
			}
			w.mu.Unlock()

			if !alreadyHadConnection {
				// First peer connected - start audio (optional, graceful failure if no devices)
				log.Println("DEBUG: Starting audio capture (VM mic -> browser)")
			if err := w.audioCapture.Start(); err != nil {
				log.Printf("WARNING: Audio capture not available (no mic?), continuing video-only: %v", err)
			} else {
				log.Println("Audio capture started (VM mic -> browser)")
			}

			log.Println("DEBUG: Initializing audio playback decoder (browser mic -> VM speaker)")
			if err := w.audioPlayback.StartDecoder(); err != nil {
				log.Printf("WARNING: Audio playback not available (no speaker?), continuing video-only: %v", err)
			} else {
				log.Println("Audio playback decoder initialized, waiting for browser audio track...")
			}
			}

			// Start ROS subscriber for this specific track (multi-track architecture)
			if w.useROSMode && exists && trackInfo.rosSubscriber != nil {
				if err := trackInfo.rosSubscriber.Start(); err != nil {
					log.Printf("ERROR: Failed to start ROS subscriber for track %s: %v", trackID, err)
				} else {
					log.Printf("ROS subscriber started for track %s (camera %d)", trackID, trackInfo.cameraNumber)
				}
			} else if w.useCameraMode && w.cameraCapture != nil {
				// Start camera capture on first connection
				if err := w.cameraCapture.Start(); err != nil {
					log.Printf("ERROR: Failed to start camera: %v", err)
				} else {
					log.Println("Camera streaming started")
				}
			} else if w.videoStreamer != nil {
				w.videoStreamer.StartStreaming()
			}

		case webrtc.PeerConnectionStateDisconnected, webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			log.Printf("[%s] WebRTC disconnected", peerID)

			w.mu.Lock()
			trackID := w.peerToTrack[peerID]
			trackInfo, trackExists := w.tracks[trackID]

			// Check if any other peers are using the same track
			trackStillInUse := false
			// Check if ANY peers are still connected (for audio management)
			hasConnected := false
			for otherPeerID, otherTrackID := range w.peerToTrack {
				if otherPeerID != peerID {
					if pc, exists := w.peerConnections[otherPeerID]; exists {
						if pc.ConnectionState() == webrtc.PeerConnectionStateConnected {
							hasConnected = true
							if otherTrackID == trackID {
								trackStillInUse = true
							}
						}
					}
				}
			}
			w.mu.Unlock()

			// Stop specific track's ROS subscriber when no peers use it (multi-track architecture)
			if !trackStillInUse && trackExists {
				log.Printf("No peers using track %s, stopping ROS subscriber", trackID)
				if w.useROSMode && trackInfo.rosSubscriber != nil {
					trackInfo.rosSubscriber.Stop()
				} else if w.useCameraMode && w.cameraCapture != nil {
					w.cameraCapture.Stop()
				} else if w.videoStreamer != nil {
					w.videoStreamer.StopStreaming()
				}
			}

			// Stop audio when NO peers are connected at all (audio is shared across all peers)
			if !hasConnected {
// 				log.Println("No peers connected, stopping audio (if running)")
// 				if w.audioCapture != nil {
// 					w.audioCapture.Stop()
// 				}
// 				if w.audioPlayback != nil {
// 					w.audioPlayback.Stop()
// 				}
			}
		}
	})

	// Store the peer connection
	w.peerConnections[peerID] = peerConnection

	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offerSDP,
	}

	// Set the remote description (offer)
	err = peerConnection.SetRemoteDescription(offer)
	if err != nil {
		return "", err
	}

	// Create an answer
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		return "", err
	}

	// Set the local description (answer)
	err = peerConnection.SetLocalDescription(answer)
	if err != nil {
		return "", err
	}

	log.Println("Created WebRTC answer")
	return answer.SDP, nil
}

func (w *WebRTCManager) AddICECandidate(peerID string, candidateData ICECandidateMessage) error {
	w.mu.Lock()
	peerConnection, exists := w.peerConnections[peerID]
	w.mu.Unlock()

	if !exists {
		log.Printf("No peer connection found for %s", peerID)
		return fmt.Errorf("no peer connection for %s", peerID)
	}

	candidate := webrtc.ICECandidateInit{
		Candidate:     candidateData.Candidate,
		SDPMid:        &candidateData.SDPMid,
		SDPMLineIndex: &candidateData.SDPMLineIndex,
	}

	err := peerConnection.AddICECandidate(candidate)
	if err != nil {
		return err
	}

	// log.Println("Added ICE candidate")
	return nil
}

func (w *WebRTCManager) SetupICECandidateHandler(peerID string, handler func(*webrtc.ICECandidate)) {
	w.mu.Lock()
	peerConnection, exists := w.peerConnections[peerID]
	w.mu.Unlock()

	if !exists {
		log.Printf("No peer connection found for %s when setting up ICE handler", peerID)
		return
	}

	peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate != nil {
			handler(candidate)
		}
	})
}

// SwitchCameraForPeer switches the camera for a specific peer
func (w *WebRTCManager) SwitchCameraForPeer(peerID string, cameraNumber int) error {
	log.Printf("SwitchCameraForPeer called for peer %s with camera number: %d", peerID, cameraNumber)

	if cameraNumber < 1 || cameraNumber > 7 {
		return fmt.Errorf("invalid camera number: %d (must be 1-7)", cameraNumber)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// Get track ID for this peer
	trackID, exists := w.peerToTrack[peerID]
	if !exists {
		return fmt.Errorf("peer %s not found", peerID)
	}

	// Get track info
	trackInfo, exists := w.tracks[trackID]
	if !exists {
		return fmt.Errorf("track %s not found for peer %s", trackID, peerID)
	}

	// Check if already on this camera
	if trackInfo.cameraNumber == cameraNumber {
		log.Printf("Track %s already on camera %d, no switch needed", trackID, cameraNumber)
		return nil
	}

	// ROS mode - switch ROS subscriber
	if w.useROSMode && trackInfo.rosSubscriber != nil {
		log.Printf("Track %s: stopping current subscriber (camera %d)", trackID, trackInfo.cameraNumber)
		trackInfo.rosSubscriber.Stop()

		// Create new subscriber with different topic
		trackInfo.rosSubscriber = NewROSSubscriber(trackInfo.track, cameraNumber, w.rosMasterURI, trackID)
		trackInfo.cameraNumber = cameraNumber

		// Check if custom topic is defined for this camera number
		if customTopic, exists := w.customTopics[cameraNumber]; exists {
			trackInfo.rosSubscriber.topicName = customTopic
			log.Printf("Track %s: using custom topic for camera %d: %s", trackID, cameraNumber, customTopic)
		} else {
			log.Printf("Track %s: using default topic for camera %d: %s", trackID, cameraNumber, trackInfo.rosSubscriber.topicName)
		}

		// Check if any peers using this track are connected
		trackInUse := false
		for otherPeerID, otherTrackID := range w.peerToTrack {
			if otherTrackID == trackID {
				if pc, exists := w.peerConnections[otherPeerID]; exists {
					if pc.ConnectionState() == webrtc.PeerConnectionStateConnected {
						trackInUse = true
						break
					}
				}
			}
		}

		if trackInUse {
			log.Printf("Track %s: starting subscriber for camera %d", trackID, cameraNumber)
			if err := trackInfo.rosSubscriber.Start(); err != nil {
				return fmt.Errorf("failed to start ROS subscriber for camera %d: %v", cameraNumber, err)
			}
			log.Printf("Successfully switched track %s to camera %d", trackID, cameraNumber)
		} else {
			log.Printf("Track %s: no connected peers, will start subscriber when client connects", trackID)
		}

		return nil
	}

	return fmt.Errorf("camera switching not supported in current mode")
}

// SwitchCamera switches camera globally (deprecated, kept for backward compatibility)
func (w *WebRTCManager) SwitchCamera(cameraNumber int) error {
	log.Printf("SwitchCamera called with camera number: %d", cameraNumber)

	// ROS mode - switch between ROS topics
	if w.useROSMode && w.rosSubscriber != nil {
		if cameraNumber < 1 || cameraNumber > 8 {
			return fmt.Errorf("invalid camera number: %d (must be 1-8)", cameraNumber)
		}

		log.Printf("ROS mode: stopping current subscriber")
		w.rosSubscriber.Stop()

		// Create new subscriber with different topic (legacy single-track mode)
		w.rosSubscriber = NewROSSubscriber(w.videoTrack, cameraNumber, w.rosMasterURI, "legacy")

		// Check if custom topic is defined for this camera number
		if customTopic, exists := w.customTopics[cameraNumber]; exists {
			w.rosSubscriber.topicName = customTopic
			log.Printf("Using custom topic for camera %d: %s", cameraNumber, customTopic)
		} else {
			log.Printf("Using default topic for camera %d: %s", cameraNumber, w.rosSubscriber.topicName)
		}

		// Check if any peers are connected, if so start the new subscriber
		w.mu.Lock()
		hasConnected := false
		for _, pc := range w.peerConnections {
			if pc.ConnectionState() == webrtc.PeerConnectionStateConnected {
				hasConnected = true
				break
			}
		}
		w.mu.Unlock()

		if hasConnected {
			log.Printf("ROS mode: starting subscriber for camera %d", cameraNumber)
			if err := w.rosSubscriber.Start(); err != nil {
				return fmt.Errorf("failed to start ROS subscriber for camera %d: %v", cameraNumber, err)
			}
			log.Printf("Successfully switched to camera %d", cameraNumber)
		} else {
			log.Printf("No connected peers, will start subscriber when client connects")
		}

		return nil
	}

	// In camera mode, all cameras use the same live camera for now
	if w.useCameraMode {
		log.Printf("Camera mode: all camera IDs use live camera (switching not implemented yet)")
		return nil
	}

	// File mode - switch between video files
	cameraMap := map[int]string{
		1: "h264/flir_id8_image_resized_30fps",
		2: "h264/leopard_id1_image_resized_30fps",
		3: "h264/leopard_id3_image_resized_30fps",
		4: "h264/leopard_id4_image_resized_30fps",
		5: "h264/leopard_id5_image_resized_30fps",
		6: "h264/leopard_id6_image_resized_30fps",
		7: "h264/leopard_id7_image_resized_30fps",
	}

	directory, ok := cameraMap[cameraNumber]
	if !ok {
		return fmt.Errorf("invalid camera number: %d (must be 1-7)", cameraNumber)
	}

	log.Printf("Switching to camera %d: %s", cameraNumber, directory)

	// Load new H.264 files
	if err := w.videoStreamer.LoadH264Files(directory); err != nil {
		return fmt.Errorf("failed to load camera %d files: %v", cameraNumber, err)
	}

	log.Printf("Successfully loaded files for camera %d from: %s", cameraNumber, directory)
	return nil
}

func (w *WebRTCManager) DisconnectPeer(peerID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if peerConnection, exists := w.peerConnections[peerID]; exists {
		log.Printf("Disconnecting peer: %s", peerID)
		err := peerConnection.Close()
		delete(w.peerConnections, peerID)

		// Get track ID and check if any other peers are using it
		trackID, hasTrack := w.peerToTrack[peerID]
		delete(w.peerToTrack, peerID) // Remove peer-to-track mapping

		// Check if any other peers are using the same track
		if hasTrack {
			trackStillInUse := false
			for otherPeerID, otherTrackID := range w.peerToTrack {
				if otherTrackID == trackID {
					if pc, exists := w.peerConnections[otherPeerID]; exists {
						if pc.ConnectionState() == webrtc.PeerConnectionStateConnected {
							trackStillInUse = true
							break
						}
					}
				}
			}

			if !trackStillInUse {
				log.Printf("No peers using track %s after disconnect, stopping ROS subscriber", trackID)
				if trackInfo, exists := w.tracks[trackID]; exists {
					if w.useROSMode && trackInfo.rosSubscriber != nil {
						trackInfo.rosSubscriber.Stop()
					} else if w.useCameraMode && w.cameraCapture != nil {
						w.cameraCapture.Stop()
					} else if w.videoStreamer != nil {
						w.videoStreamer.StopStreaming()
					}
				}
			}
		}

		return err
	}

	log.Printf("Peer %s not found", peerID)
	return nil
}

func (w *WebRTCManager) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Close all peer connections
	for peerID, peerConnection := range w.peerConnections {
		log.Printf("Closing peer connection: %s", peerID)
		peerConnection.Close()
	}
	w.peerConnections = make(map[string]*webrtc.PeerConnection)
	w.peerToTrack = make(map[string]string)

	// Stop all track subscribers
	for trackID, trackInfo := range w.tracks {
		log.Printf("Stopping ROS subscriber for track: %s", trackID)
		if w.useROSMode && trackInfo.rosSubscriber != nil {
			trackInfo.rosSubscriber.Stop()
		}
	}
	w.tracks = make(map[string]*TrackInfo)

	// Legacy cleanup for backward compatibility
	if w.useROSMode && w.rosSubscriber != nil {
		w.rosSubscriber.Stop()
	} else if w.useCameraMode && w.cameraCapture != nil {
		w.cameraCapture.Stop()
	} else if w.videoStreamer != nil {
		w.videoStreamer.StopStreaming()
	}

	return nil
}
