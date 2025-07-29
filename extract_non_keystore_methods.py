#!/usr/bin/env python3
import re

# Read the service.go file
with open('vms/xvm/service.go', 'r') as f:
    content = f.read()

# Methods to keep (non-keystore)
methods_to_keep = [
    'GetBlock',
    'GetBlockByHeight', 
    'GetHeight',
    'IssueTx',
    'GetAddressTxs',
    'GetTxStatus',
    'GetTx',
    'GetUTXOs',
    'GetAssetDescription',
    'GetBalance',
    'GetAllBalances',
]

# Extract the header up to the first method
header_match = re.search(r'^(.*?)(\nfunc \(s \*Service\))', content, re.DOTALL)
if not header_match:
    print("Could not find header")
    exit(1)

new_content = header_match.group(1)

# Extract each method to keep
for method in methods_to_keep:
    # Pattern to match the entire function including nested braces
    pattern = rf'(\nfunc \(s \*Service\) {method}\([^{{]*\{{(?:[^{{}}]|\{{[^{{}}]*\}})*\}})'
    match = re.search(pattern, content, re.DOTALL)
    if match:
        new_content += match.group(1)
    else:
        print(f"Warning: Could not find method {method}")

# Write the cleaned content
with open('vms/xvm/service_cleaned.go', 'w') as f:
    f.write(new_content)

print("Created service_cleaned.go with only non-keystore methods")