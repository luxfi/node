#!/bin/bash
# Script to verify the consensus refactoring was successful

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo "=== Consensus Refactoring Verification ==="

# Check for remaining old imports
check_old_imports() {
    echo -e "\n${YELLOW}Checking for remaining old imports...${NC}"
    
    OLD_PATTERNS=(
        "snow/consensus/snowball"
        "snow/consensus/snowman"
        "snow/consensus/snowstorm"
        "snow/consensus/lux"
        "snow/choices"
    )
    
    found_old=0
    for pattern in "${OLD_PATTERNS[@]}"; do
        echo -n "Checking for $pattern... "
        count=$(grep -r "$pattern" --include="*.go" "$PROJECT_ROOT" 2>/dev/null | grep -v "snow_backup" | wc -l || echo "0")
        if [ "$count" -gt 0 ]; then
            echo -e "${RED}Found $count occurrences${NC}"
            found_old=1
            # Show first few occurrences
            grep -r "$pattern" --include="*.go" "$PROJECT_ROOT" 2>/dev/null | grep -v "snow_backup" | head -3
        else
            echo -e "${GREEN}Clean${NC}"
        fi
    done
    
    return $found_old
}

# Check new structure exists
check_new_structure() {
    echo -e "\n${YELLOW}Verifying new directory structure...${NC}"
    
    REQUIRED_DIRS=(
        "consensus/binaryvote"
        "consensus/chain"
        "consensus/chain/poll"
        "consensus/chain/bootstrap"
        "consensus/dag"
        "consensus/common/choices"
    )
    
    all_exist=0
    for dir in "${REQUIRED_DIRS[@]}"; do
        if [ -d "$PROJECT_ROOT/$dir" ]; then
            echo -e "  ✓ $dir ${GREEN}exists${NC}"
        else
            echo -e "  ✗ $dir ${RED}missing${NC}"
            all_exist=1
        fi
    done
    
    return $all_exist
}

# Run compilation check
check_compilation() {
    echo -e "\n${YELLOW}Checking compilation...${NC}"
    
    cd "$PROJECT_ROOT"
    
    # Try to build
    if go build -v ./consensus/... 2>&1 | grep -q "error"; then
        echo -e "${RED}Compilation errors found${NC}"
        go build -v ./consensus/... 2>&1 | grep "error" | head -10
        return 1
    else
        echo -e "${GREEN}Consensus packages compile successfully${NC}"
        return 0
    fi
}

# Run tests
run_consensus_tests() {
    echo -e "\n${YELLOW}Running consensus tests...${NC}"
    
    cd "$PROJECT_ROOT"
    
    test_failed=0
    
    # Test each package
    for pkg in binaryvote chain dag common; do
        echo -n "Testing consensus/$pkg... "
        if go test "./consensus/$pkg/..." -short >/dev/null 2>&1; then
            echo -e "${GREEN}PASS${NC}"
        else
            echo -e "${RED}FAIL${NC}"
            test_failed=1
        fi
    done
    
    return $test_failed
}

# Check for import cycles
check_import_cycles() {
    echo -e "\n${YELLOW}Checking for import cycles...${NC}"
    
    cd "$PROJECT_ROOT"
    
    if go list -f '{{.ImportPath}} -> {{join .Imports " "}}' ./consensus/... 2>&1 | grep -q "import cycle"; then
        echo -e "${RED}Import cycles detected${NC}"
        go list -f '{{.ImportPath}} -> {{join .Imports " "}}' ./consensus/... 2>&1 | grep "import cycle"
        return 1
    else
        echo -e "${GREEN}No import cycles found${NC}"
        return 0
    fi
}

# Main verification
main() {
    cd "$PROJECT_ROOT"
    
    total_errors=0
    
    # Run all checks
    check_new_structure || ((total_errors++))
    check_old_imports || ((total_errors++))
    check_compilation || ((total_errors++))
    check_import_cycles || ((total_errors++))
    run_consensus_tests || ((total_errors++))
    
    echo -e "\n${YELLOW}=== Verification Summary ===${NC}"
    
    if [ $total_errors -eq 0 ]; then
        echo -e "${GREEN}✓ All checks passed!${NC}"
        echo -e "${GREEN}The consensus refactoring appears successful.${NC}"
        
        echo -e "\n${YELLOW}Next steps:${NC}"
        echo "1. Run full test suite: go test ./..."
        echo "2. Run integration tests"
        echo "3. Test with actual node operation"
        echo "4. Once verified, remove old directories:"
        echo "   rm -rf snow/consensus/snowball snow/consensus/snowman snow/consensus/snowstorm snow/consensus/lux snow/choices"
    else
        echo -e "${RED}✗ Found $total_errors issues${NC}"
        echo -e "${RED}Please fix the issues before proceeding.${NC}"
        
        if [ -f "$PROJECT_ROOT/.last_consensus_backup" ]; then
            backup_dir=$(cat "$PROJECT_ROOT/.last_consensus_backup")
            echo -e "\n${YELLOW}To rollback:${NC}"
            echo "1. rm -rf consensus"
            echo "2. cp -r $backup_dir/consensus snow/"
            echo "3. cp -r $backup_dir/choices snow/"
            echo "4. Run import updater with reverse mappings"
        fi
        
        exit 1
    fi
}

# Run verification
main