#!/bin/bash

# Run streaming with correct library path
export LD_LIBRARY_PATH=$(pwd):$LD_LIBRARY_PATH
./streaming "$@"