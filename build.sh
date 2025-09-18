#!/bin/bash

# Build script for Jetson Nano streaming backend Docker image

IMAGE_NAME="streaming-backend-jetson"
IMAGE_TAG="latest"

echo "Building Docker image for Jetson Nano with ROS Melodic..."
echo "Image name: ${IMAGE_NAME}:${IMAGE_TAG}"

# Build the Docker image
docker build \
    --platform linux/arm64 \
    -t ${IMAGE_NAME}:${IMAGE_TAG} \
    -f Dockerfile \
    .

if [ $? -eq 0 ]; then
    echo "Docker image built successfully: ${IMAGE_NAME}:${IMAGE_TAG}"
    echo "You can now run the container using ./run.sh"
else
    echo "Failed to build Docker image"
    exit 1
fi