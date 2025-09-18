import os
import re

def fix_file(filepath):
    with open(filepath, 'r') as f:
        content = f.read()
    
    original = content
    
    # Fix lux imports
    content = re.sub(r'lux"github\.com/luxfi/metric"', '"github.com/luxfi/metric"', content)
    
    # Fix duplicate metric imports
    if '"github.com/luxfi/metric"' in content and '"github.com/luxfi/node/utils/metric"' in content:
        content = re.sub(r'"github\.com/luxfi/node/utils/metric"', 'utilmetric "github.com/luxfi/node/utils/metric"', content)
        
        # Fix NewAverager references
        content = re.sub(r'\bmetric\.NewAveragerWithErrs\b', 'utilmetric.NewAveragerWithErrs', content)
        content = re.sub(r'\bmetric\.NewAverager\b', 'utilmetric.NewAverager', content)
        content = re.sub(r'\bmetric\.Averager\b', 'utilmetric.Averager', content)
    
    # Fix t.metric to t.metrics
    content = re.sub(r'\bt\.metric\.', 't.metrics.', content)
    
    if content != original:
        with open(filepath, 'w') as f:
            f.write(content)
        return True
    return False

# Fix all Go files
fixed = 0
for root, dirs, files in os.walk('.'):
    # Skip .git and vendor directories
    if '.git' in root or 'vendor' in root:
        continue
    
    for file in files:
        if file.endswith('.go'):
            filepath = os.path.join(root, file)
            if fix_file(filepath):
                fixed += 1

print(f"Fixed {fixed} files")
