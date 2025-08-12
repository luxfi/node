#!/usr/bin/env python3
"""
Complete migration from prometheus to luxfi/metric
"""

import os
import re
import sys

# Files to skip - infrastructure/API files that need prometheus
SKIP_FILES = {
    'api/metrics/',
    'api/server/server.go',  # May need prometheus for HTTP handlers
    'node/node.go',  # May need prometheus for HTTP handlers
}

def should_skip_file(filepath):
    """Check if file should be skipped"""
    for skip_pattern in SKIP_FILES:
        if skip_pattern in filepath:
            return True
    return False

def fix_imports(content):
    """Fix import statements"""
    lines = content.split('\n')
    new_lines = []
    in_imports = False
    import_added = False
    
    for line in lines:
        # Remove prometheus imports
        if 'github.com/prometheus/client_golang/prometheus' in line:
            if not 'collectors' in line and not 'promhttp' in line:
                continue  # Skip this line
        
        # Handle import blocks
        if line.strip() == 'import (' or 'import (' in line:
            in_imports = True
            new_lines.append(line)
            continue
        elif in_imports and line.strip() == ')':
            # Add luxfi/metric if needed and not already added
            if not import_added and 'metrics.' in content:
                new_lines.append('\t"github.com/luxfi/metric"')
                import_added = True
            in_imports = False
            new_lines.append(line)
            continue
        
        new_lines.append(line)
    
    return '\n'.join(new_lines)

def fix_metrics_usage(content):
    """Fix metrics usage patterns"""
    # Replace prometheus.Registerer with metrics.Registry or metrics.Metrics
    content = re.sub(r'prometheus\.Registerer', 'metrics.Registry', content)
    content = re.sub(r'prometheus\.Registry', 'metrics.Registry', content)
    
    # Replace prometheus metric types with metrics types
    content = re.sub(r'prometheus\.Counter\b', 'metrics.Counter', content)
    content = re.sub(r'prometheus\.CounterOpts\b', 'metrics.CounterOpts', content)
    content = re.sub(r'prometheus\.Gauge\b', 'metrics.Gauge', content)
    content = re.sub(r'prometheus\.GaugeOpts\b', 'metrics.GaugeOpts', content)
    content = re.sub(r'prometheus\.Histogram\b', 'metrics.Histogram', content)
    content = re.sub(r'prometheus\.HistogramOpts\b', 'metrics.HistogramOpts', content)
    content = re.sub(r'prometheus\.Summary\b', 'metrics.Summary', content)
    content = re.sub(r'prometheus\.SummaryOpts\b', 'metrics.SummaryOpts', content)
    
    # Replace prometheus.NewCounter with registry.NewCounter
    content = re.sub(r'prometheus\.NewCounter\(', 'metricsInstance.NewCounter(', content)
    content = re.sub(r'prometheus\.NewGauge\(', 'metricsInstance.NewGauge(', content)
    content = re.sub(r'prometheus\.NewHistogram\(', 'metricsInstance.NewHistogram(', content)
    content = re.sub(r'prometheus\.NewSummary\(', 'metricsInstance.NewSummary(', content)
    
    # Replace MustRegister calls
    content = re.sub(r'registerer\.MustRegister\([^)]+\)', '', content)
    content = re.sub(r'registry\.MustRegister\([^)]+\)', '', content)
    
    # Replace prometheus.NewRegistry() with metrics instance
    content = re.sub(r'prometheus\.NewRegistry\(\)', 'metrics.NewRegistry()', content)
    
    # Fix metric creation patterns
    content = re.sub(r'prometheus\.NewCounterVec\(', 'metrics.NewCounterVec(', content)
    content = re.sub(r'prometheus\.NewGaugeVec\(', 'metrics.NewGaugeVec(', content)
    
    return content

def process_file(filepath):
    """Process a single Go file"""
    if should_skip_file(filepath):
        print(f"Skipping {filepath}")
        return False
        
    try:
        with open(filepath, 'r') as f:
            content = f.read()
        
        # Check if file needs processing
        if 'prometheus' not in content and 'metrics.' not in content:
            return False
        
        original = content
        
        # Fix imports
        content = fix_imports(content)
        
        # Fix metrics usage
        content = fix_metrics_usage(content)
        
        # Write back if changed
        if content != original:
            with open(filepath, 'w') as f:
                f.write(content)
            print(f"Updated {filepath}")
            return True
        
        return False
    except Exception as e:
        print(f"Error processing {filepath}: {e}")
        return False

def find_go_files(directory):
    """Find all Go files in directory"""
    go_files = []
    for root, dirs, files in os.walk(directory):
        # Skip vendor directory
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
    
    print(f"Finding Go files in {directory}...")
    go_files = find_go_files(directory)
    
    print(f"Found {len(go_files)} Go files")
    
    updated_count = 0
    for filepath in go_files:
        if process_file(filepath):
            updated_count += 1
    
    print(f"\nUpdated {updated_count} files")

if __name__ == '__main__':
    main()