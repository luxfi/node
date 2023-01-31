<<<<<<< HEAD
// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
=======
// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
>>>>>>> 53a8245a8 (Update consensus)
// See the file LICENSE for licensing terms.

package lux

import (
	"fmt"

<<<<<<< HEAD
	"github.com/luxdefi/luxd/snow/consensus/snowball"
=======
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
>>>>>>> 53a8245a8 (Update consensus)
)

// Parameters the lux parameters include the snowball parameters and the
// optimal number of parents
type Parameters struct {
	snowball.Parameters
	Parents   int `json:"parents" yaml:"parents"`
	BatchSize int `json:"batchSize" yaml:"batchSize"`
}

// Valid returns nil if the parameters describe a valid initialization.
func (p Parameters) Valid() error {
	switch {
	case p.Parents <= 1:
		return fmt.Errorf("parents = %d: Fails the condition that: 1 < Parents", p.Parents)
	case p.BatchSize <= 0:
		return fmt.Errorf("batchSize = %d: Fails the condition that: 0 < BatchSize", p.BatchSize)
	default:
		return p.Parameters.Verify()
	}
}
