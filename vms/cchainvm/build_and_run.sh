#!/bin/bash
# Build and run the EVM to C-Chain import tool

set -e

echo "Building import tool..."

# Navigate to the directory
cd /home/z/work/lux/node/vms/cchainvm

# Build the standalone import tool
echo "Compiling import tool..."
go build -o import_tool \
    -tags standalone \
    import.go \
    import_integration.go

if [ $? -eq 0 ]; then
    echo "✅ Build successful!"
    echo ""
    echo "You can now run the import with:"
    echo "  ./import_tool"
    echo ""
    echo "Or with custom parameters:"
    echo "  ./import_tool -source /path/to/subnet/db -target /path/to/cchain/db -end 1082780"
    echo ""
    echo "For configuration file:"
    echo "  ./import_tool -config import_config.json"
else
    echo "❌ Build failed"
    exit 1
fi

# Optional: Run immediately
if [ "$1" == "--run" ]; then
    echo ""
    echo "Starting import..."
    ./import_tool
fi