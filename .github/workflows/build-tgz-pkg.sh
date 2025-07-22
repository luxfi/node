#!/usr/bin/env bash

set -euo pipefail

LUX_ROOT=$PKG_ROOT/luxd-$TAG

mkdir -p "$LUX_ROOT"

OK=$(cp ./build/luxd "$LUX_ROOT")
if [[ $OK -ne 0 ]]; then
  exit "$OK";
fi


echo "Build tgz package..."
cd "$PKG_ROOT"
echo "Tag: $TAG"
tar -czvf "luxd-linux-$ARCH-$TAG.tar.gz" "luxd-$TAG"
aws s3 cp "luxd-linux-$ARCH-$TAG.tar.gz" "s3://$BUCKET/linux/binaries/ubuntu/$RELEASE/$ARCH/"
