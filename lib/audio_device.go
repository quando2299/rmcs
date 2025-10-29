package main

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Global flag to ensure AEC module is loaded only once
var (
	aecModuleLoaded     bool
	aecModuleLoadMutex  sync.Mutex
)

// AudioDeviceInfo contains audio device detection info
type AudioDeviceInfo struct {
	UsePulseAudio bool
	InputDevice   string
	OutputDevice  string
}

// DetectAudioDevices detects available audio system and devices
func DetectAudioDevices() *AudioDeviceInfo {
	info := &AudioDeviceInfo{
		UsePulseAudio: false,
		InputDevice:   "default",
		OutputDevice:  "default",
	}

	// Check for PulseAudio
	if checkPulseAudio() {
		info.UsePulseAudio = true
		log.Println("Audio: Using PulseAudio")
		
	// Get PulseAudio devices
	if sources := getPulseAudioSources(); len(sources) > 0 {
		// Prioritize echo-cancel source for noise reduction
		echoCancelSource := "echocancel_source"
		found := false
		
		for _, source := range sources {
			if source == echoCancelSource {
				info.InputDevice = echoCancelSource
				found = true
				log.Println("Using echo-cancel source")
				break
			}
		}
		
		if !found {
			info.InputDevice = sources[0]
			log.Printf("WARNING: echocancel_source NOT found, using %s (NO AEC!)", sources[0])
		} else {
			log.Printf("AEC Input device: %s", info.InputDevice)
		}
	}
		
		if sinks := getPulseAudioSinks(); len(sinks) > 0 {
			// Prioritize echo-cancel sink for echo cancellation
			echoCancelSink := "echocancel_sink"
			found := false
			
		for _, sink := range sinks {
			if sink == echoCancelSink {
				info.OutputDevice = echoCancelSink
				found = true
				log.Println("Using echo-cancel sink (AEC OUTPUT)")
				break
			}
		}
		
		if !found {
			info.OutputDevice = sinks[0]
			log.Printf("WARNING: echocancel_sink NOT found, using %s (NO AEC!)", sinks[0])
		} else {
			log.Printf("AEC Output device: %s", info.OutputDevice)
		}
		}
	} else if checkALSA() {
		info.UsePulseAudio = false
		log.Println("Audio: Using ALSA")
		
		// ALSA default devices
		info.InputDevice = "hw:0,0"
		info.OutputDevice = "hw:0,0"
		log.Printf("Audio: ALSA devices: %s", info.InputDevice)
	} else {
		log.Println("Warning: No audio system detected, using defaults")
	}

	return info
}

// checkPulseAudio checks if PulseAudio is available
func checkPulseAudio() bool {
	cmd := exec.Command("pactl", "info")
	err := cmd.Run()
	return err == nil
}

// checkALSA checks if ALSA is available
func checkALSA() bool {
	cmd := exec.Command("arecord", "-l")
	err := cmd.Run()
	return err == nil
}

// getPulseAudioSources gets available PulseAudio input sources
func getPulseAudioSources() []string {
	// Load AEC module FIRST (but only once globally)
	loadEchoCancelModuleOnce()
	
	// Query sources AFTER module load (with delay for device registration)
	cmd := exec.Command("pactl", "list", "short", "sources")
	output, err := cmd.Output()
	if err != nil {
		return []string{"default"}
	}

	lines := strings.Split(string(output), "\n")
	var sources []string
	for _, line := range lines {
		if line != "" && !strings.Contains(line, "monitor") {
			fields := strings.Fields(line)
			if len(fields) > 1 {
				sources = append(sources, fields[1])
			}
		}
	}

	if len(sources) == 0 {
		return []string{"default"}
	}
	
	return sources
}

// verifyAECStatus checks if AEC module is running and reports configuration
func verifyAECStatus() {
	// Check if echocancel_source exists and is running
	cmd := exec.Command("pactl", "list", "short", "sources")
	output, err := cmd.Output()
	if err != nil {
		log.Println("Cannot verify AEC status (pactl failed)")
		return
	}
	
	aecSourceFound := false
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "echocancel_source") {
			fields := strings.Fields(line)
			if len(fields) >= 6 {
				// Format: ID NAME module format channels rate status
				format := fields[3]
				channels := fields[4]
				rate := fields[5]
				log.Printf("AEC Source: %s (format: %s, %s ch, rate: %s)",
					fields[1], format, channels, rate)
				
				// Info about format (not a warning - this is expected for WebRTC AEC)
				if format == "float32le" {
					log.Printf("INFO: AEC uses float32le (WebRTC internal format), PulseAudio converts to s16le for FFmpeg")
				} else if format != "s16le" {
					log.Printf("Unexpected AEC format: %s", format)
				}
				
				// Check rate is 48kHz (closest WebRTC-supported rate to 44.1kHz hardware)
				if rate != "48000Hz" {
					log.Printf("WARNING: AEC rate %s ≠ 48000Hz (expected) - should be 48kHz!", rate)
				}
				aecSourceFound = true
			}
			break
		}
	}
	
	if !aecSourceFound {
		log.Println("WARNING: echocancel_source NOT FOUND - AEC NOT WORKING!")
	}
	
	// Check if echocancel_sink exists
	cmd = exec.Command("pactl", "list", "short", "sinks")
	output, err = cmd.Output()
	if err != nil {
		return
	}
	
	aecSinkFound := false
	lines = strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "echocancel_sink") {
			fields := strings.Fields(line)
			if len(fields) >= 6 {
				format := fields[3]
				channels := fields[4]
				rate := fields[5]
				log.Printf("AEC Sink: %s (format: %s, %s ch, rate: %s)", 
					fields[1], format, channels, rate)
				
				// Info about format (not a warning - this is expected for WebRTC AEC)
				if format == "float32le" {
					log.Printf("INFO: AEC uses float32le (WebRTC internal format), PulseAudio converts to s16le for FFmpeg")
				} else if format != "s16le" {
					log.Printf("Unexpected AEC format: %s", format)
				}
				
				// Check rate is 48kHz (closest WebRTC-supported rate to 44.1kHz hardware)
				if rate != "48000Hz" {
					log.Printf("WARNING: AEC rate %s ≠ 48000Hz (expected) - should be 48kHz!", rate)
				}
				aecSinkFound = true
			}
			break
		}
	}
	
	if !aecSinkFound {
		log.Println("WARNING: echocancel_sink NOT FOUND - AEC NOT WORKING!")
	}
	
	if aecSourceFound && aecSinkFound {
		log.Println("========== AEC MODULE VERIFIED AND ACTIVE ==========")
	}
}

// loadEchoCancelModuleOnce ensures AEC module is loaded only once
func loadEchoCancelModuleOnce() {
	aecModuleLoadMutex.Lock()
	defer aecModuleLoadMutex.Unlock()
	
	if aecModuleLoaded {
		log.Println("AEC module already loaded, skipping reload")
		return
	}
	
	loadEchoCancelModule()
	aecModuleLoaded = true
}

// loadEchoCancelModule loads PulseAudio echo cancellation module for acoustic echo cancellation
func loadEchoCancelModule() {
	// Check if module is already loaded
	checkCmd := exec.Command("pactl", "list", "modules", "short")
	output, err := checkCmd.Output()
	if err == nil && strings.Contains(string(output), "module-echo-cancel") {
		log.Println("Old PulseAudio echo-cancel module found, unloading to apply new settings...")
		
		// Parse module ID and unload
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.Contains(line, "module-echo-cancel") {
				fields := strings.Fields(line)
				if len(fields) > 0 {
					moduleID := fields[0]
					unloadCmd := exec.Command("pactl", "unload-module", moduleID)
					if err := unloadCmd.Run(); err != nil {
						log.Printf("Failed to unload old module: %v", err)
					} else {
						log.Println("Old AEC module unloaded, will reload with correct format")
					}
					break
				}
			}
		}
		
		// Wait for cleanup
		time.Sleep(300 * time.Millisecond)
	}
	
	// Get hardware devices for proper master assignment
	defaultSource := getDefaultPulseAudioSource()
	defaultSink := getDefaultPulseAudioSink()
	
	cmd := exec.Command("pactl", "load-module", "module-echo-cancel",
		"use_master_format=1",  // Use hardware format
		"aec_method=webrtc",
		"aec_args=\"extended_filter=1,delay_agnostic=1,experimental_agc=1,high_pass_filter=1\"",
		fmt.Sprintf("source_master=%s", defaultSource),  // Hardware mic
		fmt.Sprintf("sink_master=%s", defaultSink),      // Hardware speaker
		"source_name=echocancel_source",
		"sink_name=echocancel_sink",
		"rate=48000",            // WebRTC native rate (44.1k falls back to 32k)
		"channels=2")            // Stereo
	
	if err := cmd.Run(); err != nil {
		log.Printf("WARNING: Failed to load PulseAudio echo-cancel module: %v", err)
		log.Println("Continuing with standard audio capture (WILL HAVE ECHO)")
	} else {
		log.Println("PulseAudio echo-cancel module loaded successfully")
		log.Println("AEC ACTIVE: WebRTC method (rate=48000Hz - AGGRESSIVE MODE)")
		log.Println("Settings: extended_filter + delay_agnostic + experimental_agc + high_pass_filter")
		
		// Wait for devices to register (critical for detection)
		time.Sleep(200 * time.Millisecond)
		
		verifyAECStatus()
	}
}

// getDefaultPulseAudioSource gets the default hardware source (microphone)
// Dynamically finds actual hardware, excludes monitors and echocancel devices
func getDefaultPulseAudioSource() string {
	// First try to get default source
	cmd := exec.Command("pactl", "get-default-source")
	output, err := cmd.Output()
	if err == nil {
		source := strings.TrimSpace(string(output))
		// If it's a real hardware device (not monitor, not echocancel), use it
		if !strings.Contains(source, "monitor") && !strings.Contains(source, "echocancel") {
			return source
		}
	}
	
	// Default is monitor or echocancel, find first real hardware source
	cmd = exec.Command("pactl", "list", "short", "sources")
	output, err = cmd.Output()
	if err != nil {
		log.Println(" Failed to query PulseAudio sources, using 'default'")
		return "default"
	}
	
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 1 {
			sourceName := fields[1]
			// Exclude monitors (virtual capture of speaker output) and echocancel devices
			if !strings.Contains(sourceName, "monitor") && !strings.Contains(sourceName, "echocancel") {
				log.Printf(" Found hardware source: %s", sourceName)
				return sourceName
			}
		}
	}
	
	log.Println(" No hardware source found, using 'default'")
	return "default"
}

// getDefaultPulseAudioSink gets the default hardware sink (speaker)
// Dynamically finds actual hardware, excludes echocancel devices
func getDefaultPulseAudioSink() string {
	// First try to get default sink
	cmd := exec.Command("pactl", "get-default-sink")
	output, err := cmd.Output()
	if err == nil {
		sink := strings.TrimSpace(string(output))
		// If it's a real hardware device (not echocancel), use it
		if !strings.Contains(sink, "echocancel") {
			return sink
		}
	}
	
	// Default is echocancel, find first real hardware sink
	cmd = exec.Command("pactl", "list", "short", "sinks")
	output, err = cmd.Output()
	if err != nil {
		log.Println(" Failed to query PulseAudio sinks, using 'default'")
		return "default"
	}
	
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 1 {
			sinkName := fields[1]
			// Exclude echocancel devices
			if !strings.Contains(sinkName, "echocancel") {
				log.Printf(" Found hardware sink: %s", sinkName)
				return sinkName
			}
		}
	}
	
	log.Println(" No hardware sink found, using 'default'")
	return "default"
}

// getPulseAudioSinks gets available PulseAudio output sinks 
func getPulseAudioSinks() []string {
	cmd := exec.Command("pactl", "list", "short", "sinks")
	output, err := cmd.Output()
	if err != nil {
		return []string{"default"}
	}

	lines := strings.Split(string(output), "\n")
	var sinks []string
	for _, line := range lines {
		if line != "" {
			fields := strings.Fields(line)
			if len(fields) > 1 {
				sinks = append(sinks, fields[1])
			}
		}
	}

	if len(sinks) == 0 {
		return []string{"default"}
	}
	return sinks
}

// UnmuteMicrophone unmutes the microphone for audio capture 
func (a *AudioDeviceInfo) UnmuteMicrophone() error {
	if a.UsePulseAudio {
		// Unmute PulseAudio source
		cmd := exec.Command("pactl", "set-source-mute", a.InputDevice, "0")
		if err := cmd.Run(); err != nil {
			log.Printf("Warning: Failed to unmute PulseAudio microphone: %v", err)
			return err
		}
		log.Printf("Audio: Unmuted microphone: %s", a.InputDevice)
	} else {
		// Unmute ALSA capture
		cmd := exec.Command("amixer", "set", "Capture", "unmute")
		if err := cmd.Run(); err != nil {
			log.Printf("Warning: Failed to unmute ALSA microphone: %v", err)
			return err
		}
		log.Printf("Audio: Unmuted ALSA microphone")
	}
	return nil
}

// SetMicrophoneVolume sets microphone volume for AEC balance
func (a *AudioDeviceInfo) SetMicrophoneVolume() error {
	if a.UsePulseAudio {
		// Set PulseAudio source volume to 15% (VERY AGGRESSIVE for stubborn echo!)
		// EXTREMELY LOW hardware sensitivity prevents mic from picking up speaker
		// Software boost (3.5x in audio_capture.go) compensates AFTER AEC processing
		// Strategy: Minimize acoustic pickup → AEC cancels → Amplify clean signal
		cmd := exec.Command("pactl", "set-source-volume", a.InputDevice, "50%")
		if err := cmd.Run(); err != nil {
			log.Printf("Warning: Failed to set PulseAudio microphone volume: %v", err)
			return err
		}
		log.Printf(" Microphone hardware: 50%% (EXTREMELY low - aggressive echo prevention!), software: 3.5x boost")
		log.Printf("Strategy: Minimize acoustic echo → Aggressive AEC → Amplify clean voice")
	} else {
		// Set ALSA capture volume to 15%
		cmd := exec.Command("amixer", "set", "Capture", "50%")
		if err := cmd.Run(); err != nil {
			log.Printf("Warning: Failed to set ALSA microphone volume: %v", err)
			return err
		}
		log.Printf("Audio: Set ALSA microphone volume to 15%% (extremely low for aggressive AEC)")
	}
	return nil
}

// SetSpeakerVolume sets speaker volume for clear playback
func (a *AudioDeviceInfo) SetSpeakerVolume() error {
	if a.UsePulseAudio {
		// Set PulseAudio sink volume to 150% (high volume for clarity)
		// Safe because mic is only 30% (won't cause echo feedback)
		// Software also boosts 6x in audio_playback.go
		cmd := exec.Command("pactl", "set-sink-volume", a.OutputDevice, "150%")
		if err := cmd.Run(); err != nil {
			log.Printf("Warning: Failed to set PulseAudio speaker volume: %v", err)
			return err
		}
		log.Printf(" Speaker hardware: 150%% (high volume for clarity), software: 6x boost + EQ")
		log.Printf("Balance: Mic 30%% (quiet) vs Speaker 150%% (loud) = No echo feedback")
	} else {
		// Set ALSA playback volume to 100% (ALSA max)
		cmd := exec.Command("amixer", "set", "Master", "100%")
		if err := cmd.Run(); err != nil {
			log.Printf("Warning: Failed to set ALSA speaker volume: %v", err)
			return err
		}
		log.Printf("Audio: Set ALSA speaker volume to 100%%")
	}
	return nil
}