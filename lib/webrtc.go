package main

import (
	"fmt"
	"log"
	"sync"

	"github.com/pion/webrtc/v4"
)

type WebRTCManager struct {
	peerConnections map[string]*webrtc.PeerConnection
	videoTrack      *webrtc.TrackLocalStaticSample
	videoStreamer   *VideoStreamer
	mu              sync.Mutex
}

// ICECandidateMessage represents an ICE candidate from Flutter
type ICECandidateMessage struct {
	Candidate     string `json:"candidate"`
	SDPMid        string `json:"sdpMid"`
	SDPMLineIndex uint16 `json:"sdpMLineIndex"`
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

	// Create proper video streamer based on libdatachannel C++ reference
	videoStreamer := NewVideoStreamer(videoTrack)

	// Load default camera (camera 1)
	defaultCamera := 1
	cameraMap := map[int]string{
		1: "h264/flir_id8_image_resized_30fps",
		2: "h264/leopard_id1_image_resized_30fps",
		3: "h264/leopard_id3_image_resized_30fps",
		4: "h264/leopard_id4_image_resized_30fps",
		5: "h264/leopard_id5_image_resized_30fps",
		6: "h264/leopard_id6_image_resized_30fps",
		7: "h264/leopard_id7_image_resized_30fps",
	}

	if defaultDir, ok := cameraMap[defaultCamera]; ok {
		if err := videoStreamer.LoadH264Files(defaultDir); err != nil {
			log.Printf("ERROR: Failed to load default camera %d files: %v", defaultCamera, err)
			// Don't continue if no files found
		} else {
			log.Printf("Loaded default camera %d: %s", defaultCamera, defaultDir)
		}
	}

	return &WebRTCManager{
		peerConnections: make(map[string]*webrtc.PeerConnection),
		videoTrack:      videoTrack,
		videoStreamer:   videoStreamer,
	}, nil
}

func (w *WebRTCManager) ProcessOffer(peerID string, offerSDP string) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

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

	// Add the video track to the new peer connection
	_, err = peerConnection.AddTrack(w.videoTrack)
	if err != nil {
		peerConnection.Close()
		return "", err
	}

	// Set up connection state handlers
	peerConnection.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		log.Printf("[%s] ICE connection state changed: %s", peerID, state.String())
	})

	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("[%s] WebRTC connection state changed: %s", peerID, state.String())

		switch state {
		case webrtc.PeerConnectionStateConnected:
			log.Printf("[%s] WebRTC connected, starting video stream", peerID)
			w.videoStreamer.StartStreaming()
		case webrtc.PeerConnectionStateDisconnected, webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			log.Printf("[%s] WebRTC disconnected", peerID)
			// Check if any peers are still connected
			w.mu.Lock()
			hasConnected := false
			for id, pc := range w.peerConnections {
				if id != peerID && pc.ConnectionState() == webrtc.PeerConnectionStateConnected {
					hasConnected = true
					break
				}
			}
			w.mu.Unlock()

			if !hasConnected {
				log.Println("No peers connected, stopping video stream")
				w.videoStreamer.StopStreaming()
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

func (w *WebRTCManager) SwitchCamera(cameraNumber int) error {
	log.Printf("SwitchCamera called with camera number: %d", cameraNumber)

	// Map camera numbers to directories
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

		// Check if any peers are still connected
		hasConnected := false
		for _, pc := range w.peerConnections {
			if pc.ConnectionState() == webrtc.PeerConnectionStateConnected {
				hasConnected = true
				break
			}
		}

		if !hasConnected {
			log.Println("No peers connected after disconnect, stopping video stream")
			w.videoStreamer.StopStreaming()
		}

		return err
	}

	log.Printf("Peer %s not found", peerID)
	return nil
}

func (w *WebRTCManager) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	for peerID, peerConnection := range w.peerConnections {
		log.Printf("Closing peer connection: %s", peerID)
		peerConnection.Close()
	}

	w.peerConnections = make(map[string]*webrtc.PeerConnection)
	w.videoStreamer.StopStreaming()
	return nil
}
