# Consensus Refactoring Plan

## Overview
This document outlines the complete plan to refactor the Snow consensus naming to a more descriptive structure.

## New Directory Structure
```
consensus/
├── binaryvote/      (was snow/consensus/snowball)
├── chain/           (was snow/consensus/snowman)
│   ├── poll/        (voting mechanisms)
│   └── bootstrap/   (bootstrapping)
├── dag/             (was snow/consensus/snowstorm + snow/consensus/lux)
│   ├── vertex.go
│   └── test_vertex.go
└── common/          (was snow/choices + shared interfaces)
    ├── choices/
    └── status.go
```

## Phase 1: Preparation (Day 1)

### 1.1 Create Migration Scripts
```bash
# Create backup
cp -r snow snow_backup_$(date +%Y%m%d_%H%M%S)

# Create new structure
mkdir -p consensus/{binaryvote,chain,dag,common}
mkdir -p consensus/chain/{poll,bootstrap}
mkdir -p consensus/common/choices
```

### 1.2 Dependency Analysis
- Map all imports using the analyze_consensus_deps.sh script
- Create import mapping file for automated replacement
- Identify circular dependencies

### 1.3 Test Baseline
```bash
# Run all tests and save results
go test ./snow/... -json > baseline_tests.json
go test ./chains/... -json >> baseline_tests.json
go test ./vms/... -json >> baseline_tests.json
```

## Phase 2: Core Refactoring (Day 2-3)

### 2.1 Move and Rename Files

#### Step 1: Binary Vote (Snowball)
```bash
# Move snowball to binaryvote
cp -r snow/consensus/snowball/* consensus/binaryvote/
# Update package names
find consensus/binaryvote -name "*.go" -exec sed -i 's/package snowball/package binaryvote/g' {} \;
```

#### Step 2: Chain Consensus (Snowman)
```bash
# Move snowman to chain
cp -r snow/consensus/snowman/* consensus/chain/
# Move subpackages
mv consensus/chain/poll consensus/chain/poll_temp
mkdir -p consensus/chain/poll
mv consensus/chain/poll_temp/* consensus/chain/poll/
rm -rf consensus/chain/poll_temp

# Update package names
find consensus/chain -name "*.go" -exec sed -i 's/package snowman/package chain/g' {} \;
find consensus/chain/poll -name "*.go" -exec sed -i 's/package poll/package poll/g' {} \;
```

#### Step 3: DAG Consensus (Snowstorm + Lux)
```bash
# Move snowstorm to dag
cp -r snow/consensus/snowstorm/* consensus/dag/
# Move lux vertex files to dag
cp snow/consensus/lux/*.go consensus/dag/

# Update package names
find consensus/dag -name "*.go" -exec sed -i 's/package snowstorm/package dag/g' {} \;
find consensus/dag -name "*.go" -exec sed -i 's/package lux/package dag/g' {} \;
```

#### Step 4: Common (Choices)
```bash
# Move choices to common
cp -r snow/choices/* consensus/common/choices/
# Update package names
find consensus/common/choices -name "*.go" -exec sed -i 's/package choices/package choices/g' {} \;
```

### 2.2 Update Imports

Create automated import updater script:

```go
// scripts/update_imports.go
package main

import (
    "os"
    "path/filepath"
    "strings"
    "io/ioutil"
    "regexp"
)

var importMappings = map[string]string{
    "github.com/luxfi/node/snow/consensus/snowball": "github.com/luxfi/node/consensus/binaryvote",
    "github.com/luxfi/node/snow/consensus/snowman": "github.com/luxfi/node/consensus/chain",
    "github.com/luxfi/node/snow/consensus/snowstorm": "github.com/luxfi/node/consensus/dag",
    "github.com/luxfi/node/snow/consensus/lux": "github.com/luxfi/node/consensus/dag",
    "github.com/luxfi/node/snow/choices": "github.com/luxfi/node/consensus/common/choices",
    "github.com/luxfi/node/snow/consensus/snowman/poll": "github.com/luxfi/node/consensus/chain/poll",
    "github.com/luxfi/node/snow/consensus/snowman/bootstrap": "github.com/luxfi/node/consensus/chain/bootstrap",
}

func main() {
    err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
        if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
            return err
        }
        
        content, err := ioutil.ReadFile(path)
        if err != nil {
            return err
        }
        
        modified := string(content)
        for old, new := range importMappings {
            modified = strings.ReplaceAll(modified, old, new)
        }
        
        if modified != string(content) {
            return ioutil.WriteFile(path, []byte(modified), info.Mode())
        }
        return nil
    })
    
    if err != nil {
        panic(err)
    }
}
```

## Phase 3: Interface Updates (Day 4)

### 3.1 Update Type Names
- Rename `snowball.Consensus` → `binaryvote.Consensus`
- Rename `snowman.Consensus` → `chain.Consensus`
- Rename `snowstorm.Consensus` → `dag.Consensus`

### 3.2 Update Factory Methods
```go
// Before: factory.NewSnowball() → factory.NewBinaryVote()
// Before: factory.NewSnowman() → factory.NewChain()
```

## Phase 4: Testing and Verification (Day 5)

### 4.1 Incremental Testing
```bash
# Test each package after refactoring
go test ./consensus/binaryvote/...
go test ./consensus/chain/...
go test ./consensus/dag/...
go test ./consensus/common/...
```

### 4.2 Integration Testing
```bash
# Run full test suite
go test ./...

# Compare with baseline
go test ./snow/... -json > post_refactor_tests.json
# Use a JSON diff tool to compare results
```

### 4.3 Build Verification
```bash
# Full build
go build -v ./...

# Check for any unresolved imports
go mod tidy
go mod verify
```

## Phase 5: Engine and VM Updates (Day 6)

### 5.1 Update Engine Imports
- Update all files in `snow/engine/` 
- Update VM implementations in `vms/`
- Update chain manager in `chains/`

### 5.2 Update Configuration
- Update config files referencing consensus parameters
- Update any hardcoded strings

## Phase 6: Documentation and Cleanup (Day 7)

### 6.1 Update Documentation
- Update all README files
- Update code comments
- Update API documentation

### 6.2 Remove Old Structure
```bash
# After all tests pass
rm -rf snow/consensus/snowball
rm -rf snow/consensus/snowman
rm -rf snow/consensus/snowstorm
rm -rf snow/consensus/lux
rm -rf snow/choices
```

## Rollback Plan

If issues arise:
```bash
# Restore from backup
rm -rf consensus
mv snow_backup_[timestamp]/consensus snow/consensus
mv snow_backup_[timestamp]/choices snow/choices

# Revert all import changes
git checkout -- .
```

## Verification Checklist

- [ ] All tests pass
- [ ] No compilation errors
- [ ] No import cycles
- [ ] Performance benchmarks unchanged
- [ ] Integration tests pass
- [ ] Manual testing of key features
- [ ] Documentation updated

## Risk Mitigation

1. **Import Cycles**: Run `go list -f '{{.ImportPath}} -> {{join .Imports " "}}' ./...` to detect cycles
2. **Missing Updates**: Use `grep -r "snow.*consensus" --include="*.go"` to find stragglers
3. **Performance**: Run benchmarks before and after
4. **Compatibility**: Test against external packages that depend on this code

## Success Criteria

1. All existing tests pass without modification (only import paths change)
2. Code is more self-documenting
3. No performance regression
4. Clean separation between chain and DAG consensus
5. Easier onboarding for new developers