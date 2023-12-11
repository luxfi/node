#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Luxgo root folder
LUX_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the constants
source "$LUX_PATH"/scripts/constants.sh

# build_image_from_remote.sh is deprecated
source "$LUX_PATH"/scripts/build_local_image.sh
