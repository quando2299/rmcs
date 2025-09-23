#!/bin/bash

echo "Fixing H264 files to add Flutter-compatible SEI timestamps..."

# Create directory for fixed files
mkdir -p h264_files_with_flutter_sei

# Process each file
for file in leopard_id7_image_resized_30fps/*.h264; do
    filename=$(basename "$file")
    echo "Processing $filename..."
    ../bag_processor/convert_to_length_prefixed "$file" "h264_files_with_flutter_sei/$filename"
done

echo "Done! Use h264_files_with_flutter_sei directory in Go code"