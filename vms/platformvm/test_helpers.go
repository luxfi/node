// Copyright (C) 2019-2025, Lux Industries Inc All rights reserved.
// See the file LICENSE for licensing terms.

//go:build test

package platformvm

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/secp256k1fx"
)

// GetNetOwners returns the owners of the given subnet (stub for tests)
func GetNetOwners(client *Client, ctx context.Context, subnetID ids.ID) (map[ids.ID]interface{}, error) {
	// Stub implementation for tests
	return map[ids.ID]interface{}{
		subnetID: &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{},
		},
	}, nil
}
