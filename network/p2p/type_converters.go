// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"github.com/luxfi/ids"
	consensusversion "github.com/luxfi/consensus/version"
	nodeset "github.com/luxfi/math/set"
	nodeversion "github.com/luxfi/node/version"
)

// toConsensusVersion converts a node version.Application to consensus version.Application
func toConsensusVersion(v *nodeversion.Application) *consensusversion.Application {
	if v == nil {
		return nil
	}
	return &consensusversion.Application{
		Name:  v.Name,
		Major: v.Major,
		Minor: v.Minor,
		Patch: v.Patch,
	}
}

// toNodeVersion converts a consensus version.Application to node version.Application
func toNodeVersion(v *consensusversion.Application) *nodeversion.Application {
	if v == nil {
		return nil
	}
	return &nodeversion.Application{
		Name:  v.Name,
		Major: v.Major,
		Minor: v.Minor,
		Patch: v.Patch,
	}
}

// copySet copies a node Set
func copySet(s nodeset.Set[ids.NodeID]) nodeset.Set[ids.NodeID] {
	result := nodeset.NewSet[ids.NodeID](s.Len())
	for nodeID := range s {
		result.Add(nodeID)
	}
	return result
}
