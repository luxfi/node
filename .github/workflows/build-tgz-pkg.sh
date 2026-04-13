#!/usr/bin/env bash

set -euo pipefail

# Create build directory structure
LUXD_ROOT=$PKG_ROOT/build

mkdir -p "$LUXD_ROOT"

OK=$(cp ./build/luxd "$LUXD_ROOT/")
if [[ $OK -ne 0 ]]; then
  exit "$OK";
fi


echo "Build tgz package..."
cd "$PKG_ROOT"
echo "Tag: $TAG"

# Create package with CLI-compatible naming: node-linux-{arch}-{version}.tar.gz
tar -czvf "node-linux-$ARCH-$TAG.tar.gz" -C "$PKG_ROOT" build

# Upload to S3 if BUCKET is set (optional)
if [[ -n "${BUCKET:-}" ]]; then
  aws s3 cp "node-linux-$ARCH-$TAG.tar.gz" "s3://$BUCKET/linux/binaries/ubuntu/$RELEASE/$ARCH/" || echo "Warning: S3 upload failed (credentials may not be configured)"
fi

# Also copy to workspace for artifact upload
mkdir -p "${GITHUB_WORKSPACE:-$(pwd)}/luxd-pkg"
cp "node-linux-$ARCH-$TAG.tar.gz" "${GITHUB_WORKSPACE:-$(pwd)}/luxd-pkg/"
