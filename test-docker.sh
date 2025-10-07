#!/bin/bash

# Build and test RMCS in Docker with ROS network
# This allows testing ROS subscriber with 7 custom topics
#
# Usage: ./test-docker.sh [topic1] [topic2] [topic3] [topic4] [topic5] [topic6] [topic7]
#
# Examples:
#   ./test-docker.sh  # Use default topics from rosbag
#   ./test-docker.sh /leopard/id1/image_resized /leopard/id3/image_resized /leopard/id4/image_resized /leopard/id5/image_resized /leopard/id6/image_resized /leopard/id7/image_resized /flir/id8/image_resized

# Default topics matching rosbag file (note: id2 is missing in rosbag, so we use id3-id7 + id8)
TOPIC1="${1:-/leopard/id1/image_resized}"
TOPIC2="${2:-/leopard/id3/image_resized}"
TOPIC3="${3:-/leopard/id4/image_resized}"
TOPIC4="${4:-/leopard/id5/image_resized}"
TOPIC5="${5:-/leopard/id6/image_resized}"
TOPIC6="${6:-/leopard/id7/image_resized}"
TOPIC7="${7:-/flir/id8/image_resized}"

echo "Building Docker image..."
docker build -f Dockerfile.test -t rmcs-test .

echo ""
echo "Running RMCS in Docker (connected to ros-network)..."
echo "Make sure roscore and rosbag player are running first!"
echo ""
echo "Camera topic mapping:"
echo "  Camera 1: $TOPIC1"
echo "  Camera 2: $TOPIC2"
echo "  Camera 3: $TOPIC3"
echo "  Camera 4: $TOPIC4"
echo "  Camera 5: $TOPIC5"
echo "  Camera 6: $TOPIC6"
echo "  Camera 7: $TOPIC7"
echo ""
echo "Press Ctrl+C to stop"
echo ""

docker run -it --rm \
  --name rmcs-test \
  --network ros-network \
  -e ROS_HOSTNAME=rmcs-test \
  -e ROS_TOPIC_1="$TOPIC1" \
  -e ROS_TOPIC_2="$TOPIC2" \
  -e ROS_TOPIC_3="$TOPIC3" \
  -e ROS_TOPIC_4="$TOPIC4" \
  -e ROS_TOPIC_5="$TOPIC5" \
  -e ROS_TOPIC_6="$TOPIC6" \
  -e ROS_TOPIC_7="$TOPIC7" \
  rmcs-test