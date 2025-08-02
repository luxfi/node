// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/luxfi/node/v2/version"
)

//go:embed COMPATIBILITY.md
var Compatibility []byte

var (
	// GitCommit is set by the build script
	GitCommit string
	// Version is the version of MPCVM
	Version string = "0.1.0"
)

type mpcvmVersion struct {
	Database string `json:"database"`
	Node     string `json:"node"`
}

func init() {
	v := &mpcvmVersion{
		Database: version.Current.String(),
		Node:     version.Current.String(),
	}
	versionBytes, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Errorf("failed to marshal version: %w", err))
	}
	Version = string(versionBytes)
}

var versionEnabled = func() (bool, error) { return true, nil }