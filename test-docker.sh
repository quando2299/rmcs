#!/bin/bash

# Build and test RMCS in Docker with ROS network
# This allows testing ROS subscriber without host networking issues

echo "Building Docker image..."
docker build -f Dockerfile.test -t rmcs-test .

echo ""
echo "Running RMCS in Docker (connected to ros-network)..."
echo "Make sure roscore and rosbag player are running first!"
echo "Press Ctrl+C to stop"
echo ""

docker run -it --rm \
  --name rmcs-test \
  --network ros-network \
  -e ROS_HOSTNAME=rmcs-test \
  rmcs-test