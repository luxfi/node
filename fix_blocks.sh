#!/bin/bash

# Function to add InitializeWithContext to a block type
add_initialize_to_block() {
    local file="$1"
    local block_type="$2"
    
    # Check if InitializeWithContext already exists
    if grep -q "func.*${block_type}.*InitializeWithContext" "$file"; then
        echo "InitializeWithContext already exists for $block_type in $file"
        return
    fi
    
    echo "Adding InitializeWithContext to $block_type in $file"
    
    # Find the last method and add InitializeWithContext after it
    # This adds it before the closing of the file
    cat >> "$file" << EOF

// InitializeWithContext initializes the block with consensus context
func (b *${block_type}) InitializeWithContext(ctx context.Context, chainCtx *consensus.Context) error {
    // Initialize any context-dependent fields here
    return nil
}
EOF
}

# Add context and consensus imports if needed
add_imports() {
    local file="$1"
    
    # Check if context import exists
    if ! grep -q '"context"' "$file"; then
        # Add context import after package declaration
        sed -i '/^package /a\\nimport "context"' "$file"
    fi
    
    # Check if consensus import exists
    if ! grep -q '"github.com/luxfi/consensus"' "$file"; then
        # Check if there's an import block
        if grep -q "^import (" "$file"; then
            # Add to existing import block
            sed -i '/^import (/a\\t"github.com/luxfi/consensus"' "$file"
        else
            # Add after context import
            sed -i '/import "context"/a\import "github.com/luxfi/consensus"' "$file"
        fi
    fi
}

# Process platformvm blocks
echo "Processing platformvm blocks..."

# Abort blocks
add_imports "vms/platformvm/block/abort_block.go"
add_initialize_to_block "vms/platformvm/block/abort_block.go" "BanffAbortBlock"
add_initialize_to_block "vms/platformvm/block/abort_block.go" "ApricotAbortBlock"

# Commit blocks
add_imports "vms/platformvm/block/commit_block.go"
add_initialize_to_block "vms/platformvm/block/commit_block.go" "BanffCommitBlock"
add_initialize_to_block "vms/platformvm/block/commit_block.go" "ApricotCommitBlock"

# Proposal blocks
add_imports "vms/platformvm/block/proposal_block.go"
add_initialize_to_block "vms/platformvm/block/proposal_block.go" "BanffProposalBlock"
add_initialize_to_block "vms/platformvm/block/proposal_block.go" "ApricotProposalBlock"

# Standard blocks
add_imports "vms/platformvm/block/standard_block.go"
add_initialize_to_block "vms/platformvm/block/standard_block.go" "BanffStandardBlock"
add_initialize_to_block "vms/platformvm/block/standard_block.go" "ApricotStandardBlock"

# Atomic block
add_imports "vms/platformvm/block/atomic_block.go"
add_initialize_to_block "vms/platformvm/block/atomic_block.go" "ApricotAtomicBlock"

# Process xvm blocks
echo "Processing xvm blocks..."
add_imports "vms/xvm/block/standard_block.go"
add_initialize_to_block "vms/xvm/block/standard_block.go" "StandardBlock"

echo "Done!"