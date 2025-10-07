#!/bin/bash

# Script to check ROS topics from 農研機構's ROS Master
# Run this on Jetson to discover available topics

if [ "$#" -ne 1 ]; then
    echo "Usage: $0 <ros-master-ip>"
    echo "Example: $0 192.168.1.100"
    exit 1
fi

ROS_MASTER_IP="$1"

echo "Checking ROS topics from: $ROS_MASTER_IP:11311"
echo ""

# Check if rostopic is installed
if ! command -v rostopic &> /dev/null; then
    echo "ERROR: rostopic command not found!"
    echo ""
    echo "ROS tools are not installed on this Jetson."
    echo "To install ROS tools, run:"
    echo ""
    echo "  sudo apt-get update"
    echo "  sudo apt-get install ros-melodic-ros-base"
    echo "  echo 'source /opt/ros/melodic/setup.bash' >> ~/.bashrc"
    echo "  source ~/.bashrc"
    echo ""
    echo "After installation, run this script again."
    exit 1
fi

# Set ROS Master URI
export ROS_MASTER_URI="http://$ROS_MASTER_IP:11311"

echo "Listing all available topics..."
echo "================================"
rostopic list

echo ""
echo "Looking for image topics..."
echo "================================"
rostopic list | grep -i image

echo ""
echo "To get info about a specific topic, run:"
echo "  export ROS_MASTER_URI=http://$ROS_MASTER_IP:11311"
echo "  rostopic info /topic/name"
echo "  rostopic hz /topic/name"
echo ""
