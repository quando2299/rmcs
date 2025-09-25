#!/bin/bash

echo "Fixing H264 files to add Flutter-compatible SEI with REAL timestamps..."

# Find the latest extracted images directory
IMAGES_DIR=$(ls -1d ../bag_processor/extracted_images_* | tail -1)
if [ ! -d "$IMAGES_DIR" ]; then
    echo "ERROR: No extracted_images directory found!"
    exit 1
fi

echo "Using images from: $IMAGES_DIR"

# Build the timestamp injection tool (for Jetson Nano - using C++17 filesystem)
cd ../bag_processor
g++ -std=c++17 inject_real_timestamps_to_h264.cpp sei_generator.cpp -o inject_real_timestamps_to_h264
cd ../backend-rmcs

# Process ALL camera directories
PROCESSED_COUNT=0
FAILED_COUNT=0

for dir in *_image_resized_30fps; do
    if [ -d "$dir" ]; then
        CAMERA_NAME=${dir%_30fps}  # Remove _30fps suffix to get leopard_id7_image_resized
        if [ -d "$IMAGES_DIR/$CAMERA_NAME" ]; then
            CAMERA_DIR="$IMAGES_DIR/$CAMERA_NAME"
            H264_DIR="$dir"

            echo "Processing camera: $CAMERA_NAME"
            echo "  Camera images: $CAMERA_DIR"
            echo "  H264 files: $H264_DIR"

            # Create output directory for this camera
            OUTPUT_DIR="h264_files_with_flutter_sei_${CAMERA_NAME}"
            mkdir -p "$OUTPUT_DIR"

            # Use the tool to inject real timestamps
            echo "  Injecting real timestamps..."
            ../bag_processor/inject_real_timestamps_to_h264 "$CAMERA_DIR" "$H264_DIR" "$OUTPUT_DIR"

            if [ $? -eq 0 ]; then
                echo "  ✓ SUCCESS for $CAMERA_NAME -> $OUTPUT_DIR"
                PROCESSED_COUNT=$((PROCESSED_COUNT + 1))
            else
                echo "  ✗ ERROR: Timestamp injection failed for $CAMERA_NAME!"
                FAILED_COUNT=$((FAILED_COUNT + 1))
            fi
            echo ""
        else
            echo "Warning: No matching images directory for $dir"
        fi
    fi
done

echo "========================================="
echo "Processing complete!"
echo "  Processed: $PROCESSED_COUNT camera(s)"
echo "  Failed: $FAILED_COUNT camera(s)"
echo "========================================="

if [ $PROCESSED_COUNT -eq 0 ]; then
    echo "ERROR: No cameras were processed successfully!"
    exit 1
else
    echo "SUCCESS! Use h264_files_with_flutter_sei_* directories in Go code"
fi