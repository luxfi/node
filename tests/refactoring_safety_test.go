// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMockPackageNamesMatchDirectories ensures that all mock packages have names matching their directory
func TestMockPackageNamesMatchDirectories(t *testing.T) {
	require := require.New(t)

	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		
		// Skip vendor and .git directories
		if d.IsDir() && (d.Name() == "vendor" || d.Name() == ".git") {
			return filepath.SkipDir
		}

		// Check mock directories
		if d.IsDir() && strings.Contains(d.Name(), "mock") {
			// Check all .go files in the mock directory
			mockFiles, err := filepath.Glob(filepath.Join(path, "*.go"))
			if err != nil {
				return err
			}

			for _, file := range mockFiles {
				// Skip test files
				if strings.HasSuffix(file, "_test.go") {
					continue
				}

				// Parse the file to get package name
				fset := token.NewFileSet()
				node, err := parser.ParseFile(fset, file, nil, parser.PackageClauseOnly)
				if err != nil {
					continue
				}

				// Check that package name matches directory name
				expectedPkg := d.Name()
				actualPkg := node.Name.Name
				
				require.Equal(expectedPkg, actualPkg, 
					"Mock package name mismatch in %s: expected %s, got %s", 
					file, expectedPkg, actualPkg)
			}
		}
		return nil
	})
	require.NoError(err)
}

// TestGenesisBlockConsistency ensures Genesis blocks use consistent IDs across packages
func TestGenesisBlockConsistency(t *testing.T) {
	require := require.New(t)

	// Check that all Genesis blocks use ids.Empty
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() && strings.HasSuffix(d.Name(), ".go") && !strings.HasSuffix(d.Name(), "_test.go") {
			content, err := filepath.ReadFile(path)
			if err != nil {
				return nil
			}

			// Look for Genesis ID definitions
			if strings.Contains(string(content), "GenesisID") && 
			   strings.Contains(string(content), "ids.GenerateTestID()") &&
			   !strings.Contains(path, "tests/") {
				t.Errorf("Found non-deterministic GenesisID in %s. Consider using ids.Empty for consistency", path)
			}
		}
		return nil
	})
	require.NoError(err)
}

// TestNoCircularImports checks for potential circular import patterns
func TestNoCircularImports(t *testing.T) {
	require := require.New(t)

	// Map of package to its imports
	imports := make(map[string][]string)

	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() && strings.HasSuffix(d.Name(), ".go") && !strings.HasSuffix(d.Name(), "_test.go") {
			fset := token.NewFileSet()
			node, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if err != nil {
				return nil
			}

			pkg := filepath.Dir(path)
			for _, imp := range node.Imports {
				if imp.Path != nil {
					impPath := strings.Trim(imp.Path.Value, "\"")
					if strings.HasPrefix(impPath, "github.com/luxfi/node/v2/node") {
						imports[pkg] = append(imports[pkg], impPath)
					}
				}
			}
		}
		return nil
	})
	require.NoError(err)

	// Check for import cycles (simplified check)
	for pkg, deps := range imports {
		for _, dep := range deps {
			// Check if the dependency imports back to the original package
			if depImports, ok := imports[dep]; ok {
				for _, depDep := range depImports {
					if strings.Contains(depDep, pkg) {
						t.Logf("Potential circular import: %s -> %s -> %s", pkg, dep, depDep)
					}
				}
			}
		}
	}
}

// TestMockGenerateCommands ensures all mock generation commands use correct package names
func TestMockGenerateCommands(t *testing.T) {
	require := require.New(t)

	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() && strings.Contains(d.Name(), "generate") && strings.HasSuffix(d.Name(), ".go") {
			content, err := filepath.ReadFile(path)
			if err != nil {
				return nil
			}

			// Check go:generate comments
			lines := strings.Split(string(content), "\n")
			for _, line := range lines {
				if strings.Contains(line, "//go:generate") && strings.Contains(line, "mockgen") {
					// Extract package name from the generate command
					if strings.Contains(line, "-package=") {
						parts := strings.Split(line, "-package=")
						if len(parts) > 1 {
							pkgPart := strings.Fields(parts[1])[0]
							// Remove ${GOPACKAGE} prefix if present
							pkgName := strings.TrimPrefix(pkgPart, "${GOPACKAGE}")
							
							// Check that it doesn't use old naming patterns
							if strings.Contains(pkgName, "chainmock") {
								t.Errorf("Found outdated chainmock reference in %s: %s", path, line)
							}
						}
					}
				}
			}
		}
		return nil
	})
	require.NoError(err)
}

// TestBuildFunctionConsistency ensures BuildChain/BuildLinear/BuildDescendants are used correctly
func TestBuildFunctionConsistency(t *testing.T) {
	require := require.New(t)

	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() && strings.HasSuffix(d.Name(), "_test.go") {
			content, err := filepath.ReadFile(path)
			if err != nil {
				return nil
			}

			// Check for old BuildChain usage with lineartest
			if strings.Contains(string(content), "lineartest.BuildChain") {
				t.Errorf("Found deprecated lineartest.BuildChain in %s. Use BuildLinear or BuildDescendants instead", path)
			}

			// Check for correct usage of BuildDescendants
			if strings.Contains(string(content), "BuildDescendants(lineartest.Genesis,") {
				// Parse to check if it's being used correctly
				lines := strings.Split(string(content), "\n")
				for i, line := range lines {
					if strings.Contains(line, "BuildDescendants(lineartest.Genesis,") && 
					   strings.Contains(line, "initializeVMWithBlockchain") {
						t.Logf("Warning: %s:%d might need BuildLinear instead of BuildDescendants when initializing VM", path, i+1)
					}
				}
			}
		}
		return nil
	})
	require.NoError(err)
}

// TestImportAliasConsistency checks that imports use consistent aliases
func TestImportAliasConsistency(t *testing.T) {
	// Common aliases that should be consistent
	expectedAliases := map[string]string{
		"github.com/luxfi/node/v2/quasar/consensustest": "",  // Should not have alias
		"github.com/luxfi/node/v2/quasar/linear/lineartest": "", // Should not have alias
		"github.com/luxfi/node/v2/quasar/linear/linearmock": "", // Should not have alias
	}

	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() && strings.HasSuffix(d.Name(), ".go") {
			fset := token.NewFileSet()
			node, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if err != nil {
				return nil
			}

			for _, imp := range node.Imports {
				if imp.Path != nil {
					impPath := strings.Trim(imp.Path.Value, "\"")
					if expectedAlias, ok := expectedAliases[impPath]; ok {
						actualAlias := ""
						if imp.Name != nil {
							actualAlias = imp.Name.Name
						}
						if actualAlias != expectedAlias && actualAlias != "_" {
							t.Errorf("Import alias mismatch in %s: %s imported as %q, expected %q", 
								path, impPath, actualAlias, expectedAlias)
						}
					}
				}
			}
		}
		return nil
	})
	require.NoError(err)
}
