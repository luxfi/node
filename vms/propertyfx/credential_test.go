// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package propertyfx

import (
	"testing"

	"github.com/luxdefi/luxd/vms/components/verify"
)

func TestCredentialState(t *testing.T) {
	intf := interface{}(&Credential{})
	if _, ok := intf.(verify.State); ok {
		t.Fatalf("shouldn't be marked as state")
	}
}
