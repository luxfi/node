#!/usr/bin/env python3
"""
Fix all import issues in Go files comprehensively.
"""

import os
import re

def fix_go_file(filepath):
    """Fix all import issues in a Go file."""
    with open(filepath, 'r') as f:
        content = f.read()
    
    original = content
    
    # Fix pattern 1: import block followed by standalone import on line 9
    # Example:
    # import (
    #     "context"
    # 
    # import "github.com/..."
    pattern1 = r'(import \(\s*\n\s*"[^"]+"\s*\n)\s*\nimport "([^"]+)"'
    replacement1 = r'\1\n\t"\2"\n)'
    content = re.sub(pattern1, replacement1, content)
    
    # Fix pattern 2: Double import blocks
    # import (
    #     "context"
    # 
    # import (
    #     "time"
    pattern2 = r'import \(\s*\n([^)]+)\n\nimport \(\s*\n([^)]+)\)'
    replacement2 = r'import (\n\1\n\2)'
    content = re.sub(pattern2, replacement2, content)
    
    # Fix pattern 3: import ( followed by another import ( on next lines
    pattern3 = r'import \(\s*\n\s*"[^"]+"\s*\n\s*import \('
    content = re.sub(pattern3, 'import (\n\t"context"\n', content)
    
    # Fix pattern 4: Clean up any remaining duplicate "import (" lines
    lines = content.split('\n')
    cleaned_lines = []
    prev_line = ""
    in_import_block = False
    
    for line in lines:
        stripped = line.strip()
        
        # Track if we're in an import block
        if stripped == 'import (':
            if in_import_block and prev_line.strip() == '':
                # Skip duplicate import (
                continue
            in_import_block = True
        elif stripped == ')' and in_import_block:
            in_import_block = False
        
        # Skip standalone import lines that are inside import blocks
        if in_import_block and line.startswith('import "'):
            # Convert to proper import line
            match = re.match(r'import "([^"]+)"', line)
            if match:
                cleaned_lines.append(f'\t"{match.group(1)}"')
                continue
        
        cleaned_lines.append(line)
        prev_line = line
    
    content = '\n'.join(cleaned_lines)
    
    if content != original:
        with open(filepath, 'w') as f:
            f.write(content)
        return True
    return False

def main():
    fixed_count = 0
    error_files = []
    
    # List of known problematic files
    problem_files = [
        'vms/components/verify/verification.go',
        'vms/components/lux/addresses.go',
        'vms/secp256k1fx/mint_operation.go',
        'vms/xvm/txs/initial_state.go',
        'vms/xvm/block/block.go',
        'vms/xvm/txs/executor/backend.go',
        'vms/xvm/service.go',
        'vms/xvm/fxs/fx.go',
        'chains/manager_test.go',
        'indexer/index.go',
        'vms/platformvm/block/block.go',
        'vms/platformvm/txs/executor/backend.go',
        'vms/platformvm/block/executor/acceptor_test.go',
        'vms/platformvm/utxo/handler.go',
        'vms/platformvm/fx/fx.go',
        'vms/propertyfx/burn_operation.go',
        'vms/propertyfx/mint_operation.go',
        'vms/nftfx/mint_operation.go',
        'vms/nftfx/transfer_operation.go',
    ]
    
    for file in problem_files:
        if os.path.exists(file):
            try:
                if fix_go_file(file):
                    print(f"Fixed: {file}")
                    fixed_count += 1
            except Exception as e:
                error_files.append((file, str(e)))
    
    # Also scan all Go files
    for root, dirs, files in os.walk('.'):
        dirs[:] = [d for d in dirs if d not in ['.git', 'vendor']]
        for file in files:
            if file.endswith('.go'):
                filepath = os.path.join(root, file)
                try:
                    if fix_go_file(filepath):
                        print(f"Fixed: {filepath}")
                        fixed_count += 1
                except Exception as e:
                    error_files.append((filepath, str(e)))
    
    print(f"\nFixed {fixed_count} files")
    if error_files:
        print(f"Errors in {len(error_files)} files:")
        for file, error in error_files[:5]:
            print(f"  {file}: {error}")

if __name__ == '__main__':
    main()