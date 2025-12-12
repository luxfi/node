// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"github.com/luxfi/ids"
	consensuscore "github.com/luxfi/consensus/core"
	consensusset "github.com/luxfi/consensus/utils/set"
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

// toConsensusSet converts a node Set to consensus Set
func toConsensusSet(s nodeset.Set[ids.NodeID]) consensusset.Set[ids.NodeID] {
	result := consensusset.NewSet[ids.NodeID](s.Len())
	for nodeID := range s {
		result.Add(nodeID)
	}
	return result
}

// toNodeSet converts a consensus Set to node Set
func toNodeSet(s consensusset.Set[ids.NodeID]) nodeset.Set[ids.NodeID] {
	result := nodeset.NewSet[ids.NodeID](len(s))
	for nodeID := range s {
		result.Add(nodeID)
	}
	return result
}

// sendConfigToSet converts a SendConfig to a consensus Set
// This is a temporary helper since SendConfig.NodeIDs is []interface{}
func sendConfigToSet(config consensuscore.SendConfig) consensusset.Set[ids.NodeID] {
	result := consensusset.NewSet[ids.NodeID](len(config.NodeIDs))
	for _, nodeIDInterface := range config.NodeIDs {
		if nodeID, ok := nodeIDInterface.(ids.NodeID); ok {
			result.Add(nodeID)
		}
	}
	return result
}
