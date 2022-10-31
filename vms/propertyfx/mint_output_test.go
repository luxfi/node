// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package propertyfx

import (
	"testing"

	"github.com/luxdefi/luxd/vms/components/verify"
)

func TestMintOutputState(t *testing.T) {
	intf := interface{}(&MintOutput{})
	if _, ok := intf.(verify.State); !ok {
		t.Fatalf("should be marked as state")
	}
}
