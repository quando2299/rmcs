//go:build library
// +build library

package main

/*
#include <stdlib.h>
*/
import "C"
import (
	"log"
	"os"
	"sync"
	"time"
)

var (
	rmcsInstance *RMCSInstance
	rmcsMutex    sync.Mutex
)

type RMCSInstance struct {
	client        *MQTTClient
	webrtcManager *WebRTCManager
	running       bool
}

//export RMCSInit
func RMCSInit() C.int {
	rmcsMutex.Lock()
	defer rmcsMutex.Unlock()

	if rmcsInstance != nil && rmcsInstance.running {
		log.Println("RMCS already initialized")
		return 1
	}

	log.Println("Initializing RMCS...")

	// Initialize WebRTC manager
	webrtcManager, err := NewWebRTCManager()
	if err != nil {
		log.Printf("Failed to create WebRTC manager: %v", err)
		return -1
	}

	// Initialize MQTT client
	mqttClient := NewMQTTClient(webrtcManager)
	if err := mqttClient.Connect(); err != nil {
		log.Printf("Failed to connect MQTT: %v", err)
		return -2
	}

	rmcsInstance = &RMCSInstance{
		client:        mqttClient,
		webrtcManager: webrtcManager,
		running:       true,
	}

	log.Println("RMCS initialized successfully")
	return 0
}

//export RMCSSwitchCamera
func RMCSSwitchCamera(cameraNumber C.int) C.int {
	rmcsMutex.Lock()
	defer rmcsMutex.Unlock()

	if rmcsInstance == nil || !rmcsInstance.running {
		log.Println("RMCS not initialized")
		return -1
	}

	camNum := int(cameraNumber)
	log.Printf("Switching to camera %d from C++", camNum)

	if err := rmcsInstance.webrtcManager.SwitchCamera(camNum); err != nil {
		log.Printf("Failed to switch camera: %v", err)
		return -2
	}

	return 0
}

//export RMCSStop
func RMCSStop() C.int {
	rmcsMutex.Lock()
	defer rmcsMutex.Unlock()

	if rmcsInstance == nil {
		return 0
	}

	log.Println("Stopping RMCS...")

	if rmcsInstance.client != nil {
		// Publish disconnect-tractor before stopping
		rmcsInstance.client.PublishDisconnectTractor()
		// Give time for message to send
		time.Sleep(500 * time.Millisecond)
		rmcsInstance.client.Disconnect()
	}

	if rmcsInstance.webrtcManager != nil {
		rmcsInstance.webrtcManager.Close()
	}

	rmcsInstance.running = false
	rmcsInstance = nil

	log.Println("RMCS stopped")
	return 0
}

//export RMCSGetStatus
func RMCSGetStatus() C.int {
	rmcsMutex.Lock()
	defer rmcsMutex.Unlock()

	if rmcsInstance != nil && rmcsInstance.running {
		return 1 // Running
	}
	return 0 // Not running
}

//export RMCSSetLogFile
func RMCSSetLogFile(filename *C.char) C.int {
	goFilename := C.GoString(filename)

	file, err := os.OpenFile(goFilename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return -1
	}

	log.SetOutput(file)
	return 0
}

// Required empty main for c-shared build
func main() {}