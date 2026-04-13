#!/bin/bash
# Rename types, methods, and functions to remove "App" prefix
# Targeted at p2p message types and builder functions

# 1. Rename Message Types and Fields (Getters)
find . -name "*.go" -type f -print0 | xargs -0 sed -i '' \
    -e 's/AppRequest/Request/g' \
    -e 's/AppResponse/Response/g' \
    -e 's/AppGossip/Gossip/g' \
    -e 's/AppError/Error/g' \
    -e 's/GetAppRequest/GetRequest/g' \
    -e 's/GetAppResponse/GetResponse/g' \
    -e 's/GetAppGossip/GetGossip/g' \
    -e 's/GetAppError/GetError/g'

# 2. Rename Builder Functions (Inbound/Outbound)
# Note: s/AppRequest/Request/g above already handled the suffix.
# Now we need to handle prefixes if they still exist.
# e.g. InboundAppRequest -> InboundRequest (if AppRequest matched first, it became InboundRequest)
# Check: InboundAppRequest -> InboundRequest.
# So IsInboundAppRequest -> IsInboundRequest?
# We should just ensure "InboundApp" -> "Inbound" (for any other patterns)
find . -name "*.go" -type f -print0 | xargs -0 sed -i '' \
    -e 's/InboundApp/Inbound/g' \
    -e 's/OutboundApp/Outbound/g'
