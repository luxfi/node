#!/usr/bin/env python3
"""
Final comprehensive fix for all metrics issues
"""

import os
import re
import subprocess

def fix_file(filepath):
    """Fix a single file"""
    with open(filepath, 'r') as f:
        content = f.read()
    
    original = content
    
    # Fix NewAPIInterceptor calls
    content = re.sub(
        r'metric\.NewAPIInterceptor\((\w+)\)',
        r'metric.NewAPIInterceptor(metrics.NewWithRegistry("", \1))',
        content
    )
    
    # Fix NewAverager calls with Registry
    content = re.sub(
        r'metric\.NewAverager\([^,]+,\s*[^,]+,\s*(reg|registerer|registry)\)',
        r'metric.NewAverager(\1, \2, metrics.NewWithRegistry("", \3))',
        content
    )
    
    # Fix duplicate imports
    lines = content.split('\n')
    new_lines = []
    import_seen = {}
    in_imports = False
    
    for line in lines:
        if 'import (' in line:
            in_imports = True
            import_seen = {}
        elif in_imports and line.strip() == ')':
            in_imports = False
        elif in_imports:
            # Track imports
            if '"github.com/luxfi/metric"' in line:
                if 'metrics' not in import_seen:
                    import_seen['metrics'] = True
                    if '"github.com/luxfi/node/api/metrics"' in import_seen:
                        line = '\tluxmetrics "github.com/luxfi/metric"'
                else:
                    continue  # Skip duplicate
            elif '"github.com/luxfi/node/api/metrics"' in line:
                import_seen['"github.com/luxfi/node/api/metrics"'] = True
        
        new_lines.append(line)
    
    content = '\n'.join(new_lines)
    
    # Fix metrics.NewRegistry() calls
    content = re.sub(
        r'metrics\.NewRegistry\(\)',
        r'metrics.NewPrometheusRegistry()',
        content
    )
    
    # Fix undefined luxmetrics
    content = re.sub(
        r'undefined: luxmetrics',
        r'luxmetrics',
        content
    )
    
    # Write back if changed
    if content != original:
        with open(filepath, 'w') as f:
            f.write(content)
        return True
    
    return False

def main():
    # Find all Go files
    go_files = []
    for root, dirs, files in os.walk('.'):
        if 'vendor' in root:
            continue
        for file in files:
            if file.endswith('.go'):
                go_files.append(os.path.join(root, file))
    
    # Fix all files
    for filepath in go_files:
        if fix_file(filepath):
            print(f"Fixed {filepath}")
    
    # Run goimports to fix import formatting
    print("\nRunning goimports...")
    subprocess.run(['goimports', '-w', '.'], capture_output=True)
    
    print("\nDone!")

if __name__ == '__main__':
    main()