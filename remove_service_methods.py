#!/usr/bin/env python3
import re

# Read the service.go file
with open('vms/xvm/service.go', 'r') as f:
    content = f.read()

# List of methods to remove
methods_to_remove = [
    'CreateAsset', 'buildCreateAssetTx',
    'CreateVariableCapAsset', 
    'CreateNFTAsset', 'buildCreateNFTAsset',
    'CreateAddress',
    'ListAddresses',
    'ExportKey',
    'ImportKey',
    'Send',
    'SendMultiple', 'buildSendMultiple',
    'Mint', 'buildMint',
    'SendNFT', 'buildSendNFT',
    'MintNFT', 'buildMintNFT',
    'Import', 'buildImport',
    'Export', 'buildExport',
]

# Remove each method
for method in methods_to_remove:
    # Pattern to match the entire function
    pattern = rf'^func \(s \*Service\) {method}\([^{{]*\{{[^{{}}]*(?:\{{[^{{}}]*\}}[^{{}}]*)*\}}\n'
    content = re.sub(pattern, '', content, flags=re.MULTILINE | re.DOTALL)

# Write the cleaned content
with open('vms/xvm/service_cleaned.go', 'w') as f:
    f.write(content)

print("Created service_cleaned.go with keystore methods removed")