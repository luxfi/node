#!/bin/bash

# Fix the imports in the validators package to use only luxfi packages consistently
cd /home/z/work/lux/node/consensus/validators

# Replace node/utils/crypto imports with luxfi/crypto
sed -i 's|"github.com/luxfi/node/utils/crypto/bls"|"github.com/luxfi/crypto/bls"|g' *.go
sed -i 's|"github.com/luxfi/node/ids"|"github.com/luxfi/ids"|g' *.go

# Also fix test files in subdirectories
find . -name "*.go" -type f | xargs sed -i 's|"github.com/luxfi/node/utils/crypto/bls"|"github.com/luxfi/crypto/bls"|g'
find . -name "*.go" -type f | xargs sed -i 's|"github.com/luxfi/node/ids"|"github.com/luxfi/ids"|g'

# Fix any remaining signer imports  
find . -name "*.go" -type f | xargs sed -i 's|"github.com/luxfi/node/utils/crypto/bls/signer/localsigner"|"github.com/luxfi/crypto/bls/signer/localsigner"|g'

echo "Fixed validators package imports"
