# RMCS Backend - WebRTC Streaming Library

A Go-based C-shared library for WebRTC video streaming with MQTT signaling.

## Project Structure

```
backend-rmcs/
├── lib/                   # Go source files
│   ├── main.go            # Standalone executable (when not building as library)
│   ├── rmcs_export.go     # C-exported functions for library
│   ├── webrtc.go          # WebRTC manager with multi-peer support
│   ├── mqtt_client.go     # MQTT client for signaling
│   ├── video_streamer.go  # H.264 video streaming
│   ├── h264_parser.go     # H.264 file parser
│   ├── constants.go       # Configuration constants
│   ├── go.mod             # Go module definition
│   └── go.sum             # Go dependencies
├── build/                 # Build outputs
│   ├── librmcs.so         # C-shared library
│   └── librmcs.h          # C header file
├── build.sh               # Build script for library
├── main.cpp               # C++ Main
└── Makefile               # Makefile for C++ example
```

## Building

### Build the C-shared library:
```bash
./build-lib.sh
```

This creates:
- `build/librmcs.so` - The shared library
- `build/librmcs.h` - The C header file

### Build the C++ example:
```bash
make clean && make
```

## C++ API Functions

- `RMCSInit()` - Initialize WebRTC and connect to MQTT
- `RMCSSwitchCamera(1-7)` - Switch between camera feeds
- `RMCSStop()` - Stop and cleanup (publishes disconnect-tractor)
- `RMCSGetStatus()` - Check if running (1) or stopped (0)
- `RMCSSetLogFile(filename)` - Set log output file

## MQTT Topics

### Subscribed:
- `<baseTopic>/<peerId>/offer` - WebRTC offers from frontend
- `<baseTopic>/<peerId>/candidate/robot` - ICE candidates from frontend
- `<baseTopic>/<peerId>/disconnect-client` - Disconnect specific peer
- `<thingName>/camera` - Camera switching (1-7)

### Published:
- `<baseTopic>/<peerId>/answer` - WebRTC answers
- `<baseTopic>/<peerId>/candidate/rmcs` - ICE candidates
- `<baseTopic>/disconnect-tractor` - On shutdown (message: "robot")

## Features

- Multi-peer WebRTC connections
- Dynamic camera switching (7 video feeds)
- H.264 video streaming with SEI timestamps
- Automatic disconnect handling
- Thread-safe operations