#!/bin/bash
set -e

echo "Checking and building external packages..."

# List of packages to check
PACKAGES=(
    "metrics"
    "log"
    "crypto"
    "trace"
    "bft"
)

# Base directory
BASE_DIR="/Users/z/work/lux"

for pkg in "${PACKAGES[@]}"; do
    echo "===================================="
    echo "Checking $pkg package..."
    echo "===================================="
    
    PKG_DIR="$BASE_DIR/$pkg"
    
    if [ ! -d "$PKG_DIR" ]; then
        echo "❌ Package directory not found: $PKG_DIR"
        continue
    fi
    
    cd "$PKG_DIR"
    
    # Check git status
    echo "Git status:"
    git status --short
    
    # Build the package
    echo -e "\nBuilding..."
    if go build ./...; then
        echo "✅ Build successful"
    else
        echo "❌ Build failed"
        continue
    fi
    
    # Run tests
    echo -e "\nRunning tests..."
    if go test ./... -v -short; then
        echo "✅ Tests passed"
    else
        echo "❌ Tests failed"
    fi
    
    # Check if there are uncommitted changes
    if [ -n "$(git status --porcelain)" ]; then
        echo -e "\n⚠️  Uncommitted changes detected"
    fi
    
    echo ""
done

echo "===================================="
echo "Summary complete!"