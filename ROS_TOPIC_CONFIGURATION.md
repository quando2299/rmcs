# ROS Topic Configuration Guide

## Quick Start

RMCS can subscribe to **ANY** 7 ROS image topics from any ROS Master and switch between them.

### Build (One time setup)

```bash
cd rmcs/
./build-lib.sh      # Build Go library
make clean && make  # Build C++ application
```

### Run - Connect to ROS Master with 7 Topics

**Usage:**
```bash
./run-jetson.sh <ros-master-ip> <topic1> <topic2> <topic3> <topic4> <topic5> <topic6> <topic7>
```

**Example:**
```bash
# Connect to 農研機構 ROS Master with 7 camera topics
./run-jetson.sh 192.168.1.100 \
  /leopard/id1/image_raw \
  /leopard/id2/image_raw \
  /leopard/id3/image_raw \
  /leopard/id4/image_raw \
  /leopard/id5/image_raw \
  /leopard/id6/image_raw \
  /leopard/id7/image_raw
```

**All 7 topics must be provided.** The Flutter app can switch between cameras 1-7 using the camera buttons.

## How It Works

When you run the command above:

1. Connects to ROS Master at `192.168.1.100:11311`
2. Initially subscribes to topic 1 (first topic in the list)
3. When user clicks camera button in Flutter app, switches to corresponding topic
4. Each topic receives BGR8 image messages
5. Encodes to H.264 using FFmpeg
6. Streams via WebRTC to Flutter app

## Camera Button Mapping

| Camera Button | Topic Used |
|--------------|------------|
| Camera 1     | First topic argument  |
| Camera 2     | Second topic argument |
| Camera 3     | Third topic argument  |
| Camera 4     | Fourth topic argument |
| Camera 5     | Fifth topic argument  |
| Camera 6     | Sixth topic argument  |
| Camera 7     | Seventh topic argument |

## Requirements

**ROS Topic Requirements:**
- Message Type: `sensor_msgs/Image`
- Encoding: `bgr8` (8-bit BGR color image)
- Publishing Rate: ~30 Hz recommended

**Network Requirements:**
- Jetson must be able to reach ROS Master IP
- Port 11311 must be accessible
- ROS Master must be running before starting RMCS

## Troubleshooting

**Connection failed:**
```
failed to create ROS node: unable to set Host automatically
```
→ Check ROS Master IP is correct and reachable (`ping <ip>`)

**Topic not found:**
```
ERROR: Failed to create subscriber
```
→ Check topic name is correct (ask ROS Master operator for topic name)

**Wrong encoding:**
```
WARNING: unexpected encoding rgb8 (expected bgr8)
```
→ Topic must publish `bgr8` images, not `rgb8`

**Video freezing/corrupted:**
→ Check network bandwidth and ROS topic publishing rate (`rostopic hz /topic/name`)
