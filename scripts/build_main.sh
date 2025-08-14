#!/bin/bash
set -e

echo "Building luxd main binary..."

# Create temporary module
mkdir -p /tmp/luxd-build
cd /tmp/luxd-build

# Copy main file
cp /home/z/work/lux/node/main/main.go .

# Create minimal go.mod
cat > go.mod << EOF
module luxd

go 1.21

replace (
    github.com/luxfi/node => /home/z/work/lux/node
    github.com/luxfi/consensus => /home/z/work/lux/consensus
    github.com/luxfi/crypto => /home/z/work/lux/crypto
    github.com/luxfi/database => /home/z/work/lux/database
    github.com/luxfi/geth => /home/z/work/lux/geth
    github.com/luxfi/ids => /home/z/work/lux/ids
    github.com/luxfi/log => /home/z/work/lux/log
    github.com/luxfi/metric => /home/z/work/lux/metric
    github.com/luxfi/metrics => /home/z/work/lux/metrics
    github.com/luxfi/trace => /home/z/work/lux/trace
)

require github.com/luxfi/node v0.0.0
EOF

# Build
go build -o luxd main.go

# Copy back
cp luxd /home/z/work/lux/node/build/

echo "Build complete!"
ls -la /home/z/work/lux/node/build/