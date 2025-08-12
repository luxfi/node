#!/usr/bin/env python3
"""
Complete fix for all metrics issues in the Lux node codebase
"""

import os
import re
import sys

def fix_registry_to_metrics(content):
    """Fix places where Registry is passed but Metrics is expected"""
    # Pattern to find functions that take Registry but should take Metrics
    patterns = [
        # NewAverager patterns
        (r'func\s+NewAverager\([^)]*reg(?:isterer)?\s+(?:metrics\.)?Registry[^)]*\)',
         lambda m: m.group(0).replace('Registry', 'Metrics')),
        
        # NewAveragerWithErrs patterns
        (r'func\s+NewAveragerWithErrs\([^)]*reg(?:isterer)?\s+(?:metrics\.)?Registry[^)]*\)',
         lambda m: m.group(0).replace('Registry', 'Metrics')),
        
        # Function calls passing Registry where Metrics is expected
        (r'NewAverager(?:WithErrs)?\([^,]+,\s*[^,]+,\s*(\w+)',
         lambda m: m.group(0) if 'metricsInstance' in m.group(0) else m.group(0).replace(m.group(1), f'metrics.NewWithRegistry("", {m.group(1)})')),
    ]
    
    for pattern, replacement in patterns:
        content = re.sub(pattern, replacement, content)
    
    return content

def fix_metrics_creation(content):
    """Fix metrics instance creation from Registry"""
    lines = content.split('\n')
    new_lines = []
    
    for i, line in enumerate(lines):
        # Check if we're passing a Registry where Metrics is expected
        if 'cannot use' in line and 'Registry' in line and 'Metrics value' in line:
            # This is an error line, skip it
            continue
            
        # Fix NewAverager calls with Registry
        if 'NewAverager' in line and 'registerer' in line:
            # Check if we need to create a Metrics instance
            if 'metrics.NewWithRegistry' not in line:
                # Look for the registerer variable name
                match = re.search(r'NewAverager(?:WithErrs)?\([^,]+,\s*[^,]+,\s*(\w+)', line)
                if match:
                    var_name = match.group(1)
                    if var_name in ['reg', 'registerer', 'registry']:
                        # Insert a line to create metrics instance
                        new_lines.append(f'\tmetricsInstance := metrics.NewWithRegistry("", {var_name})')
                        line = line.replace(var_name, 'metricsInstance')
        
        # Fix NewMeteredBlockState calls
        if 'NewMeteredBlockState' in line and 'metricsInstance' in line:
            # Check if metricsInstance is a Registry
            for j in range(max(0, i-10), i):
                if 'metricsInstance' in lines[j] and 'Registry' in lines[j]:
                    # Need to create a Metrics instance
                    line = line.replace('metricsInstance', 'metrics.NewWithRegistry("", metricsInstance)')
                    break
        
        new_lines.append(line)
    
    return '\n'.join(new_lines)

def fix_duplicate_imports(content):
    """Fix duplicate metrics imports"""
    lines = content.split('\n')
    new_lines = []
    in_imports = False
    seen_metrics = False
    api_metrics_seen = False
    
    for line in lines:
        if 'import (' in line:
            in_imports = True
            seen_metrics = False
            api_metrics_seen = False
        elif in_imports and line.strip() == ')':
            in_imports = False
        elif in_imports:
            if '"github.com/luxfi/metrics"' in line:
                if not seen_metrics:
                    if api_metrics_seen:
                        # Need to alias it
                        line = '\tluxmetrics "github.com/luxfi/metrics"'
                    seen_metrics = True
                else:
                    continue  # Skip duplicate
            elif '"github.com/luxfi/node/api/metrics"' in line:
                api_metrics_seen = True
        
        new_lines.append(line)
    
    return '\n'.join(new_lines)

def fix_undefined_metrics_instance(content):
    """Fix undefined metricsInstance errors"""
    if 'undefined: metricsInstance' in content or 'metricsInstance.New' in content:
        # Look for the context where metricsInstance should be defined
        lines = content.split('\n')
        new_lines = []
        
        for i, line in enumerate(lines):
            # Check if we're using metricsInstance without defining it
            if 'metricsInstance.New' in line and 'metricsInstance :=' not in '\n'.join(lines[:i]):
                # Find the registerer or registry variable
                for j in range(max(0, i-20), i):
                    if 'registerer' in lines[j] or 'registry' in lines[j] or 'reg' in lines[j]:
                        match = re.search(r'(\w+)\s+(?:metrics\.)?Registry', lines[j])
                        if match:
                            var_name = match.group(1)
                            # Insert metrics instance creation
                            new_lines.append(f'\tmetricsInstance := metrics.NewWithRegistry("", {var_name})')
                            break
                
            new_lines.append(line)
        
        return '\n'.join(new_lines)
    
    return content

def fix_prometheus_references(content):
    """Remove or replace remaining prometheus references"""
    # Replace prometheus types with metrics types
    replacements = [
        (r'prometheus\.Registry', 'metrics.Registry'),
        (r'prometheus\.Registerer', 'metrics.Registry'),
        (r'prometheus\.Counter\b', 'metrics.Counter'),
        (r'prometheus\.Gauge\b', 'metrics.Gauge'),
        (r'prometheus\.Histogram\b', 'metrics.Histogram'),
        (r'prometheus\.Summary\b', 'metrics.Summary'),
        (r'prometheus\.CounterOpts\b', 'map[string]string'),
        (r'prometheus\.GaugeOpts\b', 'map[string]string'),
        (r'prometheus\.Labels\b', 'map[string]string'),
        (r'undefined: prometheus', '// prometheus removed'),
    ]
    
    for pattern, replacement in replacements:
        content = re.sub(pattern, replacement, content)
    
    return content

def fix_syntax_errors(content, filepath):
    """Fix specific syntax errors"""
    if 'x/sync/metrics.go' in filepath:
        # Fix the specific syntax error in x/sync/metrics.go
        content = re.sub(
            r'metricsInstance\.NewCounter\(\s*Namespace:\s*namespace,\s*Name:\s*"([^"]+)",\s*Help:\s*"([^"]+)",\s*\}\)',
            r'metricsInstance.NewCounter("\1", "\2")',
            content
        )
        content = re.sub(
            r'metrics\.NewCounter\(\s*Namespace:\s*namespace,\s*Name:\s*"([^"]+)",\s*Help:\s*"([^"]+)",\s*\}\)',
            r'metrics.NewCounter("\1", "\2")',
            content
        )
    
    return content

def fix_test_metrics(content, filepath):
    """Fix test files with metrics issues"""
    if 'test' in filepath.lower():
        # Fix metrics.NewClient references
        content = re.sub(r'metrics\.NewClient\(', 'metrics.NewOptionalGatherer().Register(', content)
        
        # Fix undefined prometheus in tests
        if 'undefined: prometheus' in content:
            # Add prometheus import for tests that need it
            lines = content.split('\n')
            for i, line in enumerate(lines):
                if 'import (' in line:
                    # Add prometheus import
                    lines.insert(i+1, '\t"github.com/prometheus/client_golang/prometheus"')
                    break
            content = '\n'.join(lines)
    
    return content

def process_file(filepath):
    """Process a single Go file"""
    try:
        with open(filepath, 'r') as f:
            content = f.read()
        
        original = content
        
        # Apply all fixes
        content = fix_duplicate_imports(content)
        content = fix_registry_to_metrics(content)
        content = fix_metrics_creation(content)
        content = fix_undefined_metrics_instance(content)
        content = fix_prometheus_references(content)
        content = fix_syntax_errors(content, filepath)
        content = fix_test_metrics(content, filepath)
        
        if content != original:
            with open(filepath, 'w') as f:
                f.write(content)
            return True
        
        return False
    except Exception as e:
        print(f"Error processing {filepath}: {e}")
        return False

def find_go_files(directory):
    """Find all Go files"""
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
    
    print("Fixing all metrics issues...")
    go_files = find_go_files(directory)
    
    updated_count = 0
    for filepath in go_files:
        if process_file(filepath):
            print(f"Fixed {filepath}")
            updated_count += 1
    
    print(f"\nFixed {updated_count} files")

if __name__ == '__main__':
    main()