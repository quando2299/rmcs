#!/bin/bash

# Build script for RMCS C-shared library

echo "Building RMCS C-shared library..."

# Delete old built files
rm librmcs.*
rm build/librmcs.*

# Copy h264 folders for streaming
mv ../bag_processor/h264/ .

# Navigate to lib directory
cd lib

# Build the C-shared library
go build -buildvcs=false -tags library -buildmode=c-shared -o ../build/librmcs.so .

# Check if build was successful
if [ $? -eq 0 ]; then
    echo "Build successful!"
    echo "Library: build/librmcs.so"
    echo "Header:  build/librmcs.h"

    # Go back to root directory
    cd ..

    # Copy to root for backward compatibility (optional)
    cp build/librmcs.so librmcs.so
    cp build/librmcs.h librmcs.h

    echo "Files also copied to root directory for compatibility"
else
    echo "Build failed!"
    exit 1
fi