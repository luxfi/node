// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/luxfi/node/tests/fixture/tmpnet"
)

var (
	nodeBuildOnce sync.Once
	nodeBuildPath string
	nodeBuildErr  error
)

// EnsureNodeBinary ensures the luxd binary is built and returns its path.
// It builds the binary only once and caches the result.
func EnsureNodeBinary() (string, error) {
	nodeBuildOnce.Do(func() {
		// Check if LUXD_PATH is set
		if path := os.Getenv(tmpnet.LuxNodePathEnvName); path != "" {
			if _, err := os.Stat(path); err == nil {
				nodeBuildPath = path
				return
			}
		}

		// Check if it exists in the build directory
		repoRoot, err := findRepoRoot()
		if err != nil {
			nodeBuildErr = err
			return
		}

		buildDir := filepath.Join(repoRoot, "build")
		luxdPath := filepath.Join(buildDir, "luxd")

		if _, err := os.Stat(luxdPath); err == nil {
			nodeBuildPath = luxdPath
			return
		}

		// Build it
		if err := os.MkdirAll(buildDir, 0755); err != nil {
			nodeBuildErr = err
			return
		}

		cmd := exec.Command("go", "build", "-o", luxdPath, "./cmd/luxd")
		cmd.Dir = repoRoot
		if output, err := cmd.CombinedOutput(); err != nil {
			nodeBuildErr = err
			// Log the output for debugging
			if len(output) > 0 {
				os.Stderr.Write(output)
			}
			return
		}

		nodeBuildPath = luxdPath
	})

	return nodeBuildPath, nodeBuildErr
}

// findRepoRoot finds the repository root by looking for go.mod
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached root without finding go.mod
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
