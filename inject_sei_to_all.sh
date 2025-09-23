#!/bin/bash

# Create directory for files with SEI
mkdir -p leopard_id7_with_sei

# Inject SEI into each file
for file in leopard_id7_image_resized_30fps/*.h264; do
    filename=$(basename "$file")
    echo "Processing $filename..."
    ../bag_processor/convert_to_length_prefixed "$file" "leopard_id7_with_sei/$filename"
done

echo "Done! Files with SEI are in leopard_id7_with_sei/"