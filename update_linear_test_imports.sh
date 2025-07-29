#\!/bin/bash
echo "Updating lineartest and linearmock imports to chaintest and chainmock..."

# Update lineartest imports to chaintest
find . -name "*.go" -type f -exec sed -i 's|"github.com/luxfi/node/consensus/chain/lineartest"|"github.com/luxfi/node/consensus/chain/chaintest"|g' {} +

# Update linearmock imports to chainmock  
find . -name "*.go" -type f -exec sed -i 's|"github.com/luxfi/node/consensus/chain/linearmock"|"github.com/luxfi/node/consensus/chain/chainmock"|g' {} +

# Count how many files were potentially affected
echo "Potentially updated files:"
find . -name "*.go" -type f -exec grep -l "chaintest\|chainmock" {} + | wc -l

echo "Import update complete\!"
