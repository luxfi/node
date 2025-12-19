// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"encoding/json"
	"strconv"
	"time"

	_ "embed"
)

const (
	Client = "luxd"
	// RPCChainVMProtocol should be bumped anytime changes are made which
	// require the plugin vm to upgrade to latest node release to be
	// compatible.
	RPCChainVMProtocol uint = 42
)

// These variables are set at build time via ldflags from git tag:
//
//	go build -ldflags "-X github.com/luxfi/node/version.VersionMajor=1 \
//	                   -X github.com/luxfi/node/version.VersionMinor=22 \
//	                   -X github.com/luxfi/node/version.VersionPatch=19"
//
// Build with scripts/build.sh to automatically inject version from git tags.
var (
	VersionMajor = ""
	VersionMinor = ""
	VersionPatch = ""
)

// These are globals that describe network upgrades and node versions
var (
	Current    *Semantic
	CurrentApp *Application

	MinimumCompatibleVersion = &Application{
		Name:  Client,
		Major: 1,
		Minor: 13,
		Patch: 0,
	}
	PrevMinimumCompatibleVersion = &Application{
		Name:  Client,
		Major: 1,
		Minor: 12,
		Patch: 0,
	}

	CurrentDatabase = DatabaseVersion1_4_5
	PrevDatabase    = DatabaseVersion1_0_0

	DatabaseVersion1_4_5 = &Semantic{
		Major: 1,
		Minor: 4,
		Patch: 5,
	}
	DatabaseVersion1_0_0 = &Semantic{
		Major: 1,
		Minor: 0,
		Patch: 0,
	}

	//go:embed compatibility.json
	rpcChainVMProtocolCompatibilityBytes []byte
	// RPCChainVMProtocolCompatibility maps RPCChainVMProtocol versions to the
	// set of node versions that supported that version. This is not used
	// by node, but is useful for downstream libraries.
	RPCChainVMProtocolCompatibility map[uint][]*Semantic
)

func init() {
	// Version MUST be set via ldflags at build time from git tag
	// Build script extracts semver from: git describe --tags
	if VersionMajor == "" || VersionMinor == "" || VersionPatch == "" {
		panic("version not set: build with scripts/build.sh or pass -ldflags with VersionMajor/Minor/Patch")
	}

	major, err := strconv.Atoi(VersionMajor)
	if err != nil {
		panic("invalid VersionMajor: " + VersionMajor)
	}
	minor, err := strconv.Atoi(VersionMinor)
	if err != nil {
		panic("invalid VersionMinor: " + VersionMinor)
	}
	patch, err := strconv.Atoi(VersionPatch)
	if err != nil {
		panic("invalid VersionPatch: " + VersionPatch)
	}

	Current = &Semantic{
		Major: major,
		Minor: minor,
		Patch: patch,
	}
	CurrentApp = &Application{
		Name:  Client,
		Major: Current.Major,
		Minor: Current.Minor,
		Patch: Current.Patch,
	}

	// Parse RPC compatibility map
	var parsedRPCChainVMCompatibility map[uint][]string
	err = json.Unmarshal(rpcChainVMProtocolCompatibilityBytes, &parsedRPCChainVMCompatibility)
	if err != nil {
		panic(err)
	}

	RPCChainVMProtocolCompatibility = make(map[uint][]*Semantic)
	for rpcChainVMProtocol, versionStrings := range parsedRPCChainVMCompatibility {
		versions := make([]*Semantic, len(versionStrings))
		for i, versionString := range versionStrings {
			version, err := Parse(versionString)
			if err != nil {
				panic(err)
			}
			versions[i] = version
		}
		RPCChainVMProtocolCompatibility[rpcChainVMProtocol] = versions
	}
}

func GetCompatibility(minCompatibleTime time.Time) Compatibility {
	return NewCompatibility(
		CurrentApp,
		MinimumCompatibleVersion,
		minCompatibleTime,
		PrevMinimumCompatibleVersion,
	)
}
