#!/bin/bash
# Replay historic blockchain data into C-Chain with quantum finality

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

echo -e "${CYAN}╔═══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${CYAN}║     HISTORIC BLOCKCHAIN REPLAY - QUASAR EVENT HORIZON        ║${NC}"
echo -e "${CYAN}╚═══════════════════════════════════════════════════════════════╝${NC}\n"

# Build replay tool if needed
if [ ! -f "./build/replay-historic-chain" ]; then
    echo -e "${YELLOW}🔨 Building replay tool...${NC}"
    go build -o ./build/replay-historic-chain ./cmd/replay-historic-chain
    echo -e "${GREEN}✅ Replay tool built${NC}\n"
fi

# Available chains
STATE_DIR="$HOME/work/lux/state/chaindata"

echo -e "${BLUE}📚 Available Historic Chains:${NC}"
echo ""

declare -a CHAINS
i=1
for dir in "$STATE_DIR"/*; do
    if [ -d "$dir/db/pebbledb" ]; then
        chain_name=$(basename "$dir")
        echo -e "  ${GREEN}[$i]${NC} $chain_name"
        CHAINS[$i]=$chain_name
        ((i++))
    fi
done

echo ""

# Interactive selection if no args provided
if [ $# -eq 0 ]; then
    echo -e "${YELLOW}Select chain to replay (or press Ctrl+C to exit):${NC}"
    read -p "Choice [1-$((i-1))]: " choice

    if [ -z "${CHAINS[$choice]}" ]; then
        echo -e "${RED}❌ Invalid choice${NC}"
        exit 1
    fi

    CHAIN_NAME="${CHAINS[$choice]}"
    SOURCE="$STATE_DIR/$CHAIN_NAME/db/pebbledb"

    echo ""
    echo -e "${YELLOW}Replay range (leave empty for ALL blocks):${NC}"
    read -p "Start block [0]: " START_BLOCK
    read -p "End block [all]: " END_BLOCK

    START_BLOCK=${START_BLOCK:-0}

elif [ $# -eq 1 ]; then
    # Chain name provided
    CHAIN_NAME="$1"
    SOURCE="$STATE_DIR/$CHAIN_NAME/db/pebbledb"
    START_BLOCK=0
    END_BLOCK=""

elif [ $# -eq 2 ]; then
    # Chain name + start block
    CHAIN_NAME="$1"
    SOURCE="$STATE_DIR/$CHAIN_NAME/db/pebbledb"
    START_BLOCK="$2"
    END_BLOCK=""

elif [ $# -ge 3 ]; then
    # Full specification
    CHAIN_NAME="$1"
    SOURCE="$STATE_DIR/$CHAIN_NAME/db/pebbledb"
    START_BLOCK="$2"
    END_BLOCK="$3"
fi

# Target directory
TARGET="$HOME/.luxd/db/C-replayed"

echo ""
echo -e "${GREEN}🎯 Replay Configuration:${NC}"
echo -e "   Chain:        ${YELLOW}$CHAIN_NAME${NC}"
echo -e "   Source:       $SOURCE"
echo -e "   Target:       $TARGET"
echo -e "   Start Block:  ${CYAN}$START_BLOCK${NC}"
if [ -n "$END_BLOCK" ]; then
    echo -e "   End Block:    ${CYAN}$END_BLOCK${NC}"
else
    echo -e "   End Block:    ${CYAN}ALL${NC}"
fi

echo ""
echo -e "${YELLOW}⚠️  This will create/overwrite C-Chain database at:${NC}"
echo -e "   $TARGET"
echo ""
read -p "Continue? (y/N): " confirm

if [ "$confirm" != "y" ] && [ "$confirm" != "Y" ]; then
    echo -e "${RED}❌ Cancelled${NC}"
    exit 0
fi

# Check source exists
if [ ! -d "$SOURCE" ]; then
    echo -e "${RED}❌ Source database not found: $SOURCE${NC}"
    exit 1
fi

# Backup existing target if present
if [ -d "$TARGET" ]; then
    BACKUP="$TARGET.backup.$(date +%Y%m%d_%H%M%S)"
    echo -e "${YELLOW}📦 Backing up existing database to: $BACKUP${NC}"
    mv "$TARGET" "$BACKUP"
fi

# Create target directory
mkdir -p "$TARGET"

echo ""
echo -e "${CYAN}🌌 Pulling historic chain into Quasar event horizon...${NC}"
echo ""

# Run replay
if [ -n "$END_BLOCK" ]; then
    ./build/replay-historic-chain "$SOURCE" "$TARGET" "$START_BLOCK" "$END_BLOCK"
else
    ./build/replay-historic-chain "$SOURCE" "$TARGET" "$START_BLOCK"
fi

echo ""
echo -e "${GREEN}╔═══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║                 🎉 REPLAY SUCCESSFUL 🎉                       ║${NC}"
echo -e "${GREEN}╚═══════════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "${BLUE}📍 Next Steps:${NC}"
echo -e "   1. Move replayed database to C-Chain location:"
echo -e "      ${YELLOW}mv $TARGET ~/.luxd/db/C${NC}"
echo ""
echo -e "   2. Start node with historic state:"
echo -e "      ${YELLOW}luxd --network-id=96369${NC}"
echo ""
echo -e "${CYAN}🌌 Historic chain is now part of the Quasar event horizon!${NC}"
echo ""
