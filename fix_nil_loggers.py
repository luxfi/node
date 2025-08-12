#!/usr/bin/env python3
import os
import re
import sys

def fix_nil_loggers(filepath):
    """Fix nil loggers in test files by replacing with log.NewNoOpLogger()"""
    
    with open(filepath, 'r') as f:
        content = f.read()
    
    original_content = content
    
    # Check if log package is imported
    if 'github.com/luxfi/log' not in content:
        # Find the import block and add log import
        import_pattern = r'(import \([^)]+)'
        match = re.search(import_pattern, content)
        if match:
            # Add log import after ids or before the first node import
            if 'github.com/luxfi/ids' in content:
                content = re.sub(
                    r'(\s+"github.com/luxfi/ids")',
                    r'\1\n\t"github.com/luxfi/log"',
                    content,
                    count=1
                )
            else:
                # Add after other luxfi imports
                content = re.sub(
                    r'(import \(\n)',
                    r'\1\t"github.com/luxfi/log"\n',
                    content,
                    count=1
                )
    
    # Replace Log: nil with Log: log.NewNoOpLogger()
    patterns = [
        (r'Log:\s*nil,', 'Log: log.NewNoOpLogger(),'),
        (r'Log:\s*nil\b', 'Log: log.NewNoOpLogger()'),
        (r'logger:\s*nil,', 'logger: log.NewNoOpLogger(),'),
        (r'logger:\s*nil\b', 'logger: log.NewNoOpLogger()'),
    ]
    
    for pattern, replacement in patterns:
        content = re.sub(pattern, replacement, content)
    
    if content != original_content:
        with open(filepath, 'w') as f:
            f.write(content)
        print(f"Fixed: {filepath}")
        return True
    return False

def main():
    test_files = [
        "/home/z/work/lux/node/x/sync/client_test.go",
        "/home/z/work/lux/node/vms/rpcchainvm/vm_test.go",
        "/home/z/work/lux/node/vms/platformvm/validators/manager_benchmark_test.go",
        "/home/z/work/lux/node/vms/proposervm/pre_fork_block_test.go",
        "/home/z/work/lux/node/vms/proposervm/block_test.go",
        "/home/z/work/lux/node/vms/xvm/block/executor/block_test.go",
        "/home/z/work/lux/node/consensus/engine/dag/state/unique_vertex_test.go",
        "/home/z/work/lux/node/consensus/engine/graph/state/unique_vertex_test.go",
    ]
    
    fixed_count = 0
    for filepath in test_files:
        if os.path.exists(filepath):
            if fix_nil_loggers(filepath):
                fixed_count += 1
    
    print(f"Fixed {fixed_count} files")

if __name__ == "__main__":
    main()