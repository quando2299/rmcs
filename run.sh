#!/bin/bash

# Run script for Jetson Nano streaming backend Docker container

IMAGE_NAME="streaming-backend-jetson"
IMAGE_TAG="latest"
CONTAINER_NAME="streaming-backend"

# Check if container is already running
if [ "$(docker ps -q -f name=${CONTAINER_NAME})" ]; then
    echo "Container ${CONTAINER_NAME} is already running. Stopping it..."
    docker stop ${CONTAINER_NAME}
fi

# Remove old container if exists
if [ "$(docker ps -aq -f name=${CONTAINER_NAME})" ]; then
    echo "Removing old container..."
    docker rm ${CONTAINER_NAME}
fi

echo "Starting Docker container for streaming backend..."

# Detect platform and set runtime accordingly
RUNTIME_FLAG=""
DEVICE_FLAGS=""
NETWORK_MODE=""

# Check if running on Jetson (has nvidia runtime)
if docker info 2>/dev/null | grep -q "nvidia"; then
    echo "Detected NVIDIA runtime (Jetson device)"
    RUNTIME_FLAG="--runtime nvidia"
    DEVICE_FLAGS="--device /dev/video0:/dev/video0 --device /dev/video1:/dev/video1"
    NETWORK_MODE="--network host"
else
    echo "Running on non-Jetson device (development mode)"
    # Try using host network mode even on Mac/Linux for better WebRTC connectivity
    # Note: host mode has limitations on Mac but works better for WebRTC
    NETWORK_MODE="--network host"
    echo "Using host network mode for WebRTC compatibility"
fi

# Run the Docker container with appropriate settings
# The container will automatically build and run the streaming application
docker run -it \
    --name ${CONTAINER_NAME} \
    ${RUNTIME_FLAG} \
    ${NETWORK_MODE} \
    --privileged \
    -v $(pwd):/workspace \
    -v /tmp/.X11-unix:/tmp/.X11-unix:rw \
    -e DISPLAY=${DISPLAY} \
    -e QT_X11_NO_MITSHM=1 \
    --add-host=host.docker.internal:host-gateway \
    ${DEVICE_FLAGS} \
    ${IMAGE_NAME}:${IMAGE_TAG} \
    /bin/bash -c "echo 'Building Go application...' && go build -o rmcs . && echo 'Starting streaming server...' && ./rmcs"

echo "Container stopped."