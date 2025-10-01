#!/bin/bash

# Run RMCS on Jetson connecting to Mac's ROS Master
# Usage: ./run-jetson.sh <mac-ip-address>

if [ "$#" -ne 1 ]; then
    echo "Usage: $0 <mac-ip-address>"
    echo "Example: $0 192.168.1.100"
    exit 1
fi

MAC_IP="$1"

echo "Starting RMCS on Jetson..."
echo "Connecting to ROS Master at: $MAC_IP:11311"
echo ""

# Set ROS Master URI to point to Mac
export ROS_MASTER_URI="http://$MAC_IP:11311"

# Set library path and run
export LD_LIBRARY_PATH=$(pwd):$LD_LIBRARY_PATH
./streaming "$@"
