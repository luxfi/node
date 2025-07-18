package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

var importMappings = map[string]string{
	`"github.com/luxfi/node/snow/consensus/snowball"`:           `"github.com/luxfi/node/consensus/binaryvote"`,
	`"github.com/luxfi/node/snow/consensus/snowman"`:            `"github.com/luxfi/node/consensus/chain"`,
	`"github.com/luxfi/node/snow/consensus/snowstorm"`:          `"github.com/luxfi/node/consensus/dag"`,
	`"github.com/luxfi/node/snow/consensus/lux"`:                `"github.com/luxfi/node/consensus/dag"`,
	`"github.com/luxfi/node/snow/choices"`:                      `"github.com/luxfi/node/consensus/common/choices"`,
	`"github.com/luxfi/node/snow/consensus/snowman/poll"`:       `"github.com/luxfi/node/consensus/chain/poll"`,
	`"github.com/luxfi/node/snow/consensus/snowman/bootstrap"`:  `"github.com/luxfi/node/consensus/chain/bootstrap"`,
	`"github.com/luxfi/node/snow/consensus/snowman/bootstrapper"`: `"github.com/luxfi/node/consensus/chain/bootstrapper"`,
	
	// Also handle without quotes (in some contexts)
	`github.com/luxfi/node/snow/consensus/snowball`:           `github.com/luxfi/node/consensus/binaryvote`,
	`github.com/luxfi/node/snow/consensus/snowman`:            `github.com/luxfi/node/consensus/chain`,
	`github.com/luxfi/node/snow/consensus/snowstorm`:          `github.com/luxfi/node/consensus/dag`,
	`github.com/luxfi/node/snow/consensus/lux`:                `github.com/luxfi/node/consensus/dag`,
	`github.com/luxfi/node/snow/choices`:                      `github.com/luxfi/node/consensus/common/choices`,
	`github.com/luxfi/node/snow/consensus/snowman/poll`:       `github.com/luxfi/node/consensus/chain/poll`,
	`github.com/luxfi/node/snow/consensus/snowman/bootstrap`:  `github.com/luxfi/node/consensus/chain/bootstrap`,
}

// Type name mappings
var typeMappings = map[string]string{
	"snowball.": "binaryvote.",
	"snowman.":  "chain.",
	"snowstorm.": "dag.",
	"choices.":   "choices.",
}

func updateFile(path string) (bool, error) {
	content, err := ioutil.ReadFile(path)
	if err != nil {
		return false, err
	}

	original := string(content)
	modified := original

	// Update imports
	for old, new := range importMappings {
		modified = strings.ReplaceAll(modified, old, new)
	}

	// Update type references
	for old, new := range typeMappings {
		// Be careful to only replace type references, not random occurrences
		modified = strings.ReplaceAll(modified, old, new)
	}

	// Special cases for test packages
	modified = strings.ReplaceAll(modified, "snowmantest.", "chaintest.")
	modified = strings.ReplaceAll(modified, "snowballtest.", "binaryvotetest.")

	if modified != original {
		err = ioutil.WriteFile(path, []byte(modified), 0644)
		if err != nil {
			return false, err
		}
		return true, nil
	}

	return false, nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run update_consensus_imports.go <directory>")
		fmt.Println("Example: go run update_consensus_imports.go .")
		os.Exit(1)
	}

	root := os.Args[1]
	updatedCount := 0
	errorCount := 0

	// Read exclusions from a file if it exists
	excludeDirs := map[string]bool{
		".git":          true,
		"vendor":        true,
		"node_modules":  true,
		".idea":         true,
		"snow_backup":   true,
	}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories in exclude list
		if info.IsDir() {
			if excludeDirs[info.Name()] || strings.HasPrefix(info.Name(), "snow_backup_") {
				return filepath.SkipDir
			}
			return nil
		}

		// Only process Go files
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		// Skip if it's already in the new consensus directory
		if strings.Contains(path, "/consensus/") && !strings.Contains(path, "/snow/consensus/") {
			return nil
		}

		updated, err := updateFile(path)
		if err != nil {
			fmt.Printf("Error updating %s: %v\n", path, err)
			errorCount++
			return nil
		}

		if updated {
			fmt.Printf("Updated: %s\n", path)
			updatedCount++
		}

		return nil
	})

	if err != nil {
		fmt.Printf("Error walking directory: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n=== Summary ===\n")
	fmt.Printf("Files updated: %d\n", updatedCount)
	fmt.Printf("Errors: %d\n", errorCount)
	
	if errorCount > 0 {
		os.Exit(1)
	}
}