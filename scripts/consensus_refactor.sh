#!/bin/bash
# Automated consensus refactoring script

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "=== Consensus Refactoring Script ==="
echo "Project root: $PROJECT_ROOT"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Step 1: Create backup
backup_consensus() {
    echo -e "${YELLOW}Creating backup...${NC}"
    BACKUP_DIR="snow_backup_$(date +%Y%m%d_%H%M%S)"
    cp -r "$PROJECT_ROOT/snow" "$PROJECT_ROOT/$BACKUP_DIR"
    echo -e "${GREEN}Backup created: $BACKUP_DIR${NC}"
    echo "$BACKUP_DIR" > "$PROJECT_ROOT/.last_consensus_backup"
}

# Step 2: Create new directory structure
create_new_structure() {
    echo -e "${YELLOW}Creating new consensus directory structure...${NC}"
    
    mkdir -p "$PROJECT_ROOT/consensus/binaryvote"
    mkdir -p "$PROJECT_ROOT/consensus/chain/poll"
    mkdir -p "$PROJECT_ROOT/consensus/chain/bootstrap"
    mkdir -p "$PROJECT_ROOT/consensus/dag"
    mkdir -p "$PROJECT_ROOT/consensus/common/choices"
    
    echo -e "${GREEN}Directory structure created${NC}"
}

# Step 3: Copy files to new locations
copy_files() {
    echo -e "${YELLOW}Copying files to new structure...${NC}"
    
    # Binary Vote (Snowball)
    if [ -d "$PROJECT_ROOT/snow/consensus/snowball" ]; then
        cp -r "$PROJECT_ROOT/snow/consensus/snowball"/* "$PROJECT_ROOT/consensus/binaryvote/" 2>/dev/null || true
        echo "  - Copied snowball → binaryvote"
    fi
    
    # Chain (Snowman)
    if [ -d "$PROJECT_ROOT/snow/consensus/snowman" ]; then
        # Copy main files
        find "$PROJECT_ROOT/snow/consensus/snowman" -maxdepth 1 -name "*.go" -exec cp {} "$PROJECT_ROOT/consensus/chain/" \;
        
        # Copy poll subdirectory
        if [ -d "$PROJECT_ROOT/snow/consensus/snowman/poll" ]; then
            cp -r "$PROJECT_ROOT/snow/consensus/snowman/poll"/* "$PROJECT_ROOT/consensus/chain/poll/" 2>/dev/null || true
        fi
        
        # Copy bootstrap subdirectory
        if [ -d "$PROJECT_ROOT/snow/consensus/snowman/bootstrap" ]; then
            cp -r "$PROJECT_ROOT/snow/consensus/snowman/bootstrap"/* "$PROJECT_ROOT/consensus/chain/bootstrap/" 2>/dev/null || true
        fi
        
        echo "  - Copied snowman → chain"
    fi
    
    # DAG (Snowstorm + Lux)
    if [ -d "$PROJECT_ROOT/snow/consensus/snowstorm" ]; then
        cp -r "$PROJECT_ROOT/snow/consensus/snowstorm"/* "$PROJECT_ROOT/consensus/dag/" 2>/dev/null || true
        echo "  - Copied snowstorm → dag"
    fi
    
    if [ -d "$PROJECT_ROOT/snow/consensus/lux" ]; then
        cp "$PROJECT_ROOT/snow/consensus/lux"/*.go "$PROJECT_ROOT/consensus/dag/" 2>/dev/null || true
        echo "  - Merged lux → dag"
    fi
    
    # Common (Choices)
    if [ -d "$PROJECT_ROOT/snow/choices" ]; then
        cp -r "$PROJECT_ROOT/snow/choices"/* "$PROJECT_ROOT/consensus/common/choices/" 2>/dev/null || true
        echo "  - Copied choices → common/choices"
    fi
    
    echo -e "${GREEN}Files copied successfully${NC}"
}

# Step 4: Update package declarations
update_packages() {
    echo -e "${YELLOW}Updating package declarations...${NC}"
    
    # Update binaryvote package
    find "$PROJECT_ROOT/consensus/binaryvote" -name "*.go" -exec sed -i.bak 's/^package snowball$/package binaryvote/g' {} \;
    
    # Update chain package
    find "$PROJECT_ROOT/consensus/chain" -name "*.go" -maxdepth 1 -exec sed -i.bak 's/^package snowman$/package chain/g' {} \;
    
    # Update dag package
    find "$PROJECT_ROOT/consensus/dag" -name "*.go" -exec sed -i.bak 's/^package snowstorm$/package dag/g' {} \;
    find "$PROJECT_ROOT/consensus/dag" -name "*.go" -exec sed -i.bak 's/^package lux$/package dag/g' {} \;
    
    # Clean up backup files
    find "$PROJECT_ROOT/consensus" -name "*.bak" -delete
    
    echo -e "${GREEN}Package declarations updated${NC}"
}

# Step 5: Create import mapping file
create_import_mapping() {
    echo -e "${YELLOW}Creating import mapping file...${NC}"
    
    cat > "$PROJECT_ROOT/scripts/import_mappings.txt" << 'EOF'
github.com/luxfi/node/snow/consensus/snowball → github.com/luxfi/node/consensus/binaryvote
github.com/luxfi/node/snow/consensus/snowman → github.com/luxfi/node/consensus/chain
github.com/luxfi/node/snow/consensus/snowstorm → github.com/luxfi/node/consensus/dag
github.com/luxfi/node/snow/consensus/lux → github.com/luxfi/node/consensus/dag
github.com/luxfi/node/snow/choices → github.com/luxfi/node/consensus/common/choices
github.com/luxfi/node/snow/consensus/snowman/poll → github.com/luxfi/node/consensus/chain/poll
github.com/luxfi/node/snow/consensus/snowman/bootstrap → github.com/luxfi/node/consensus/chain/bootstrap
EOF
    
    echo -e "${GREEN}Import mapping file created${NC}"
}

# Step 6: Test the changes
run_tests() {
    echo -e "${YELLOW}Running tests on new structure...${NC}"
    
    cd "$PROJECT_ROOT"
    
    # Test each package
    echo "Testing binaryvote..."
    go test ./consensus/binaryvote/... -short || echo -e "${RED}Binaryvote tests failed${NC}"
    
    echo "Testing chain..."
    go test ./consensus/chain/... -short || echo -e "${RED}Chain tests failed${NC}"
    
    echo "Testing dag..."
    go test ./consensus/dag/... -short || echo -e "${RED}DAG tests failed${NC}"
    
    echo "Testing common..."
    go test ./consensus/common/... -short || echo -e "${RED}Common tests failed${NC}"
}

# Main execution
main() {
    echo "This script will refactor the Snow consensus structure."
    echo "A backup will be created before any changes are made."
    read -p "Continue? (y/n) " -n 1 -r
    echo
    
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Aborted."
        exit 1
    fi
    
    cd "$PROJECT_ROOT"
    
    # Execute steps
    backup_consensus
    create_new_structure
    copy_files
    update_packages
    create_import_mapping
    
    echo -e "\n${GREEN}=== Refactoring Complete ===${NC}"
    echo "Next steps:"
    echo "1. Run the import updater script to update all imports"
    echo "2. Run full test suite: go test ./..."
    echo "3. If issues occur, restore from backup: $(cat .last_consensus_backup)"
}

# Run main function
main