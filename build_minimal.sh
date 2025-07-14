#!/bin/bash

echo "Building minimal luxd..."

# Build only the main binary without tests
go build -ldflags="-s -w" -o luxd ./main/*.go 2>&1 | tee build.log

if [ -f luxd ]; then
    echo "✅ Build successful!"
    ./luxd --version
else
    echo "❌ Build failed. Checking errors..."
    tail -50 build.log
fi