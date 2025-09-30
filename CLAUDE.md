# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

RMCS (Remote Machine Control System) is a Go-based WebRTC streaming library that provides real-time H.264 video streaming with MQTT signaling. The project builds as a C-shared library that can be integrated into C++ applications.

## Architecture

### Core Components

The system consists of:
- **Go Library** (`lib/`): Core WebRTC and MQTT functionality exported as C-shared library
- **C++ Integration**: Example application using the shared library
- **Multi-peer Support**: Manages multiple simultaneous WebRTC connections
- **Camera Switching**: Supports 7 different H.264 video feeds

### Key Go Modules

- `rmcs_export.go`: C-exported functions for library interface (RMCSInit, RMCSSwitchCamera, etc.)
- `webrtc.go`: WebRTC peer connection management with multi-peer support
- `mqtt_client.go`: MQTT client for WebRTC signaling
- `video_streamer.go`: H.264 video streaming pipeline
- `h264_parser.go`: H.264 NAL unit parsing and SEI timestamp injection
- `constants.go`: MQTT broker configuration

## Development Commands

### Building the Library

```bash
# Build C-shared library (creates build/librmcs.so and build/librmcs.h)
./build-lib.sh

# Build C++ example application
make clean && make

# Run the streaming application
./run.sh
```

### Go Development

```bash
# Navigate to Go source directory
cd lib/

# Check code quality
go vet ./...

# Update dependencies
go mod tidy

# Build as standalone executable (for testing)
go build -buildvcs=false -tags library .
```

## MQTT Communication Protocol

### Topics Structure
- Base topic: `<thingName>/robot-control`
- Peer-specific topics: `<baseTopic>/<peerId>/<action>`

### Message Flow
1. Frontend sends offer to `<baseTopic>/<peerId>/offer`
2. Backend responds with answer on `<baseTopic>/<peerId>/answer`
3. ICE candidates exchanged on `candidate/robot` and `candidate/rmcs` subtopics
4. Camera switching commands on `<thingName>/camera` (values 1-7)
5. Disconnect handling via `disconnect-client` and `disconnect-tractor` topics

## H.264 Video Pipeline

The system expects H.264 files in `./h264/` directory with naming convention:
- `camera_{number}.h264` where number is 1-7
- Files are copied from `../bag_processor/h264/` during build
- SEI timestamps are injected for synchronization

## C API Reference

```c
// Initialize WebRTC and connect to MQTT
int RMCSInit();

// Switch between camera feeds (1-7)
int RMCSSwitchCamera(int cameraNumber);

// Stop and cleanup (publishes disconnect-tractor)
int RMCSStop();

// Check if running (1) or stopped (0)
int RMCSGetStatus();

// Set log output file
int RMCSSetLogFile(const char* filename);
```

## Configuration

MQTT broker settings are defined in `lib/constants.go`:
- Broker: rmcs.d6-vnext.com:1883
- Thing Name: d76053c0-6cae-47ee-b4c6-a7f96573f7e6
- Authentication: Username/password based

## Dependencies

- Go 1.21+
- Pion WebRTC v4.1.4
- Eclipse Paho MQTT v1.5.0
- C++ compiler with C++11 support