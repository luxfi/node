#!/usr/bin/env python3
import re

with open('consensus/engine/dag/getter/getter.go', 'r') as f:
    content = f.read()

# Fix NewAverager calls
content = re.sub(
    r'(\w+),\s*err\s*:=\s*NewAverager\((.*?)\)',
    r'\1 := NewAverager(\2, &errs)',
    content
)

# Add errs wrapper if not present
if 'wrappers.Errs' not in content:
    content = re.sub(
        r'(import \()',
        r'\1\n\t"github.com/luxfi/node/utils/wrappers"',
        content
    )
    
# Add errs declaration
content = re.sub(
    r'func New\((.*?)\) \(Storage, error\) \{',
    r'func New(\1) (Storage, error) {\n\terrs := &wrappers.Errs{}',
    content
)

# Return errs.Err at the end
content = re.sub(
    r'return &storage\{(.*?)\}, nil',
    r'return &storage{\1}, errs.Err',
    content
)

with open('consensus/engine/dag/getter/getter.go', 'w') as f:
    f.write(content)

print("Fixed consensus/engine/dag/getter/getter.go")
