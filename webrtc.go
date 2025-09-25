package main

import (
	"fmt"
	"log"

	"github.com/pion/webrtc/v4"
)

type WebRTCManager struct {
	peerConnection *webrtc.PeerConnection
	videoTrack     *webrtc.TrackLocalStaticSample
	videoStreamer  *VideoStreamer
}

// ICECandidateMessage represents an ICE candidate from Flutter
type ICECandidateMessage struct {
	Candidate     string `json:"candidate"`
	SDPMid        string `json:"sdpMid"`
	SDPMLineIndex uint16 `json:"sdpMLineIndex"`
}

func NewWebRTCManager() (*WebRTCManager, error) {
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	peerConnection, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return nil, err
	}

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

	// Add the video track to the peer connection
	_, err = peerConnection.AddTrack(videoTrack)
	if err != nil {
		return nil, err
	}

	// Set up connection state handlers

	peerConnection.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		log.Printf("ICE connection state changed: %s", state.String())
	})

	// Create proper video streamer based on libdatachannel C++ reference
	videoStreamer := NewVideoStreamer(videoTrack)

	// Load default camera (camera 1)
	defaultCamera := 1
	cameraMap := map[int]string{
		1: "h264_files_with_flutter_sei_flir_id8_image_resized",
		2: "h264_files_with_flutter_sei_leopard_id1_image_resized",
		3: "h264_files_with_flutter_sei_leopard_id3_image_resized",
		4: "h264_files_with_flutter_sei_leopard_id4_image_resized",
		5: "h264_files_with_flutter_sei_leopard_id5_image_resized",
		6: "h264_files_with_flutter_sei_leopard_id6_image_resized",
		7: "h264_files_with_flutter_sei_leopard_id7_image_resized",
	}

	if defaultDir, ok := cameraMap[defaultCamera]; ok {
		if err := videoStreamer.LoadH264Files(defaultDir); err != nil {
			log.Printf("ERROR: Failed to load default camera %d files: %v", defaultCamera, err)
			// Don't continue if no files found
		} else {
			log.Printf("Loaded default camera %d: %s", defaultCamera, defaultDir)
		}
	}

	// Start streaming when connection is established
	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("WebRTC connection state changed: %s", state.String())

		switch state {
		case webrtc.PeerConnectionStateConnected:
			log.Println("WebRTC connected, starting video stream")
			videoStreamer.StartStreaming()
		case webrtc.PeerConnectionStateDisconnected, webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			log.Println("WebRTC disconnected, stopping video stream")
			videoStreamer.StopStreaming()
		}
	})

	return &WebRTCManager{
		peerConnection: peerConnection,
		videoTrack:     videoTrack,
		videoStreamer:  videoStreamer,
	}, nil
}

func (w *WebRTCManager) ProcessOffer(offerSDP string) (string, error) {
	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offerSDP,
	}

	// Set the remote description (offer)
	err := w.peerConnection.SetRemoteDescription(offer)
	if err != nil {
		return "", err
	}

	// Create an answer
	answer, err := w.peerConnection.CreateAnswer(nil)
	if err != nil {
		return "", err
	}

	// Set the local description (answer)
	err = w.peerConnection.SetLocalDescription(answer)
	if err != nil {
		return "", err
	}

	log.Println("Created WebRTC answer")
	return answer.SDP, nil
}

func (w *WebRTCManager) AddICECandidate(candidateData ICECandidateMessage) error {
	candidate := webrtc.ICECandidateInit{
		Candidate:     candidateData.Candidate,
		SDPMid:        &candidateData.SDPMid,
		SDPMLineIndex: &candidateData.SDPMLineIndex,
	}

	err := w.peerConnection.AddICECandidate(candidate)
	if err != nil {
		return err
	}

	// log.Println("Added ICE candidate")
	return nil
}

func (w *WebRTCManager) SetupICECandidateHandler(handler func(*webrtc.ICECandidate)) {
	w.peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate != nil {
			handler(candidate)
		}
	})
}

func (w *WebRTCManager) SwitchCamera(cameraNumber int) error {
	log.Printf("SwitchCamera called with camera number: %d", cameraNumber)

	// Map camera numbers to directories
	cameraMap := map[int]string{
		1: "h264_files_with_flutter_sei_flir_id8_image_resized",
		2: "h264_files_with_flutter_sei_leopard_id1_image_resized",
		3: "h264_files_with_flutter_sei_leopard_id3_image_resized",
		4: "h264_files_with_flutter_sei_leopard_id4_image_resized",
		5: "h264_files_with_flutter_sei_leopard_id5_image_resized",
		6: "h264_files_with_flutter_sei_leopard_id6_image_resized",
		7: "h264_files_with_flutter_sei_leopard_id7_image_resized",
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

func (w *WebRTCManager) Close() error {
	if w.peerConnection != nil {
		return w.peerConnection.Close()
	}
	return nil
}
