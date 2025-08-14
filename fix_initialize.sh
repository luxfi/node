#!/bin/bash

# Add InitializeWithContext to all transaction types that need it

# Platform VM transactions
for file in vms/platformvm/txs/*.go; do
    if grep -q "type.*Tx struct" "$file" && ! grep -q "InitializeWithContext" "$file"; then
        # Extract the struct name
        struct_name=$(grep -o "type [A-Z][A-Za-z]*Tx struct" "$file" | awk '{print $2}')
        
        if [ ! -z "$struct_name" ]; then
            echo "Adding InitializeWithContext to $struct_name in $file"
            
            # Add the method at the end of the file before the last closing brace
            cat >> "$file" << EOF

// InitializeWithContext initializes the transaction with consensus context
func (tx *$struct_name) InitializeWithContext(ctx context.Context, chainCtx *consensus.Context) error {
    // Initialize any context-dependent fields here
    return nil
}
EOF
        fi
    fi
done

# XVM transactions  
for file in vms/xvm/txs/*.go; do
    if grep -q "type.*Tx struct" "$file" && ! grep -q "InitializeWithContext" "$file"; then
        # Extract the struct name
        struct_name=$(grep -o "type [A-Z][A-Za-z]*Tx struct" "$file" | awk '{print $2}')
        
        if [ ! -z "$struct_name" ]; then
            echo "Adding InitializeWithContext to $struct_name in $file"
            
            # Add the method at the end of the file
            cat >> "$file" << EOF

// InitializeWithContext initializes the transaction with consensus context
func (tx *$struct_name) InitializeWithContext(ctx context.Context, chainCtx *consensus.Context) error {
    // Initialize any context-dependent fields here
    return nil
}
EOF
        fi
    fi
done

echo "Done adding InitializeWithContext methods"