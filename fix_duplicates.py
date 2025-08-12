#!/usr/bin/env python3
"""
Fix duplicate imports caused by the migration script
"""

import os
import re
import sys

def fix_duplicate_imports(content):
    """Remove duplicate imports"""
    lines = content.split('\n')
    new_lines = []
    seen_imports = set()
    in_imports = False
    
    for line in lines:
        # Track import blocks
        if line.strip() == 'import (' or 'import (' in line:
            in_imports = True
            new_lines.append(line)
            seen_imports.clear()
            continue
        elif in_imports and line.strip() == ')':
            in_imports = False
            new_lines.append(line)
            seen_imports.clear()
            continue
        
        # Handle imports
        if in_imports:
            import_line = line.strip()
            if import_line and import_line not in seen_imports:
                seen_imports.add(import_line)
                new_lines.append(line)
            elif not import_line:  # Empty line
                new_lines.append(line)
        else:
            new_lines.append(line)
    
    return '\n'.join(new_lines)

def process_file(filepath):
    """Process a single Go file"""
    try:
        with open(filepath, 'r') as f:
            content = f.read()
        
        original = content
        content = fix_duplicate_imports(content)
        
        if content != original:
            with open(filepath, 'w') as f:
                f.write(content)
            print(f"Fixed {filepath}")
            return True
        
        return False
    except Exception as e:
        print(f"Error processing {filepath}: {e}")
        return False

def find_go_files(directory):
    """Find all Go files in directory"""
    go_files = []
    for root, dirs, files in os.walk(directory):
        if 'vendor' in root:
            continue
        for file in files:
            if file.endswith('.go'):
                go_files.append(os.path.join(root, file))
    return go_files

def main():
    """Main function"""
    directory = '.'
    if len(sys.argv) > 1:
        directory = sys.argv[1]
    
    print(f"Finding Go files with duplicate imports in {directory}...")
    go_files = find_go_files(directory)
    
    updated_count = 0
    for filepath in go_files:
        if process_file(filepath):
            updated_count += 1
    
    print(f"\nFixed {updated_count} files")

if __name__ == '__main__':
    main()