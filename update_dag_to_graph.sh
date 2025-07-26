#!/bin/bash
# Update DAG imports to Graph imports

echo "Updating DAG imports to Graph imports..."

# First, let's see what we have
echo "Current DAG imports:"
grep -r "engine/dag" --include="*.go" . | grep -v "vendor" | wc -l

# Create graph directory structure if it doesn't exist
if [ ! -d "consensus/engine/graph" ]; then
    echo "Creating graph directory structure..."
    cp -r consensus/engine/dag consensus/engine/graph
fi

# Update all imports from dag to graph
find . -name "*.go" -type f -exec sed -i 's|consensus/engine/dag|consensus/engine/graph|g' {} \;

# Also update any references to DAGVM to GRAPHVM
find . -name "*.go" -type f -exec sed -i 's/DAGVM/GRAPHVM/g' {} \;
find . -name "*.go" -type f -exec sed -i 's/dagVM/graphVM/g' {} \;

# Update any comments mentioning DAG
find . -name "*.go" -type f -exec sed -i 's/DAG consensus/Graph consensus/g' {} \;
find . -name "*.go" -type f -exec sed -i 's/DAG-based/Graph-based/g' {} \;

echo "Updated imports. New Graph imports:"
grep -r "engine/graph" --include="*.go" . | grep -v "vendor" | wc -l

echo "Done!"