# Release Notes - WebRTC Streaming Backend for Jetson Nano

## Version 1.0.0 - September 18, 2025

### Overview
This release provides a Dockerized WebRTC streaming backend written in Go, specifically optimized for NVIDIA Jetson Nano deployment. The application enables real-time H.264 video streaming using WebRTC with MQTT signaling, designed for robotics and IoT applications.

### Key Features
- **Go-based WebRTC Implementation**: Built with Pion WebRTC v4 for efficient, cross-platform video streaming
- **MQTT Signaling**: Uses MQTT broker for WebRTC signaling and ICE candidate exchange
- **H.264 Video Support**: Native H.264 video encoding/decoding support
- **ROS Melodic Integration**: Includes ROS Melodic for robotics applications
- **Hardware Acceleration**: Leverages Jetson Nano's GPU for video processing
- **Cross-platform Development**: Supports both development (Mac/Linux) and production (Jetson Nano) environments

### Technical Stack
- **Language**: Go 1.21
- **WebRTC Library**: Pion WebRTC v4
- **Base Image**: dustynv/ros:melodic-ros-base-l4t-r32.7.1
- **ROS Version**: Melodic
- **Video Processing**: GStreamer 1.0
- **Messaging**: MQTT (Eclipse Paho)

### Docker Base Image Selection

#### Why `dustynv/ros:melodic-ros-base-l4t-r32.7.1` Instead of `ros:melodic-ros-base-bionic`?

| Aspect | `ros:melodic-ros-base-bionic` | `dustynv/ros:melodic-ros-base-l4t-r32.7.1` |
|--------|--------------------------------|---------------------------------------------|
| **Architecture** | x86_64 (Intel/AMD) | ARM64/aarch64 (Jetson) |
| **Base OS** | Ubuntu 18.04 Bionic | NVIDIA L4T + Ubuntu 18.04 |
| **GPU Support** | None | CUDA 10.2, cuDNN, TensorRT |
| **Hardware Acceleration** | No | Yes (NVENC/NVDEC) |
| **Target Platform** | Desktop/Server | NVIDIA Jetson devices |
| **Image Size** | ~500MB | ~2GB |
| **Maintainer** | Official ROS | Community (dustynv) |

#### Critical Reasons for Using L4T Image:

1. **Architecture Compatibility**: Jetson Nano uses ARM64 processor. The standard ROS image (x86_64) will fail with "exec format error"

2. **NVIDIA Hardware Access**: L4T image provides:
   - CUDA support for GPU computing
   - Hardware video encoding (NVENC)
   - Hardware video decoding (NVDEC)
   - TensorRT for AI inference

3. **Optimized Performance**: Pre-configured for Jetson's specific hardware capabilities

4. **ROS Integration**: Includes ROS Melodic pre-installed and configured for ARM64

### Project Structure
```
backend-rmcs/
├── Dockerfile          # Multi-platform Docker configuration
├── build.sh           # Build script for Docker image
├── run.sh             # Smart run script with platform detection
├── main.go            # Go WebRTC streaming application
├── go.mod             # Go module dependencies
└── RELEASE.md         # This file
```

### Usage

#### Building the Docker Image
```bash
./build.sh
```

#### Running the Application
```bash
./run.sh
```
The application automatically:
- Detects the platform (Jetson vs Development)
- Configures appropriate networking
- Builds the Go application
- Starts the streaming server

### Platform-Specific Behavior

#### On Jetson Nano:
- Uses NVIDIA runtime for GPU access
- Enables host networking for optimal performance
- Accesses video devices (/dev/video0, /dev/video1)
- Leverages hardware acceleration

#### On Development Machine (Mac/Linux):
- Runs without NVIDIA runtime
- Uses host networking for WebRTC compatibility
- Skips video device mapping
- Suitable for testing and development

### Network Configuration
- **MQTT Broker**: test.rmcs.d6-vnext.com:1883
- **WebRTC**: Full ICE candidate support with STUN/TURN
- **Protocols**: UDP/TCP for signaling and media streams

### Known Issues and Solutions

1. **GPG Key Errors**: The Dockerfile includes fixes for expired ROS repository keys
2. **Network Isolation**: Uses host networking mode to ensure WebRTC connectivity
3. **Port Conflicts**: Automatically handled by platform detection in run.sh

### Development Notes

- The application is built fresh on each container run to ensure latest code changes
- Volume mounting allows real-time code editing without rebuilding the image
- ROS environment is automatically sourced in the container

### Future Improvements
- [ ] Implement adaptive bitrate streaming
- [ ] Add support for multiple simultaneous streams
- [ ] Create Kubernetes deployment manifests
- [ ] Add health check endpoints

### Support
For issues or questions, please refer to the project documentation or create an issue in the repository.

---
*Released on September 18, 2025*