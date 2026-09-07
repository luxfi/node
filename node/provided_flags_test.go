// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"strings"
	"testing"

	"github.com/luxfi/node/config"
)

// The boot line answers how a node was configured. Several flags carry key
// material directly — the staking TLS key, the BLS signer, the ML-DSA and
// ML-KEM keys all have *-file-content forms — and their values were going to
// the log at info, which in a container is the cluster's log store. A reader of
// logs held the node's network identity and its signing key.
func TestTheBootLineCarriesNoKeyMaterial(t *testing.T) {
	const secret = "PRIVATE-KEY-MATERIAL-DO-NOT-LOG"
	flags := map[string]interface{}{
		config.StakingTLSKeyContentKey:     secret,
		config.StakingSignerKeyContentKey:  secret,
		config.StakingMLDSAKeyContentKey:   secret,
		config.HandshakeMLKEMKeyContentKey: secret,
		config.HTTPSKeyContentKey:          secret,
		"http-port":                        9650,
	}
	got := providedFlagNames(flags)
	for _, name := range got {
		if strings.Contains(name, secret) {
			t.Fatalf("key material reached the boot line: %q", name)
		}
	}
	// The question it exists to answer is still answered.
	joined := strings.Join(got, ",")
	for want := range flags {
		if !strings.Contains(joined, want) {
			t.Fatalf("%q is missing; the line no longer says how the node was configured", want)
		}
	}
}

// Any new flag is safe by arriving. There is no list of secret-bearing names to
// keep in step, so the next key-carrying flag cannot be forgotten.
func TestAFlagAddedLaterIsSafeWithoutBeingListed(t *testing.T) {
	got := providedFlagNames(map[string]interface{}{
		"some-future-key-file-content": "A-KEY-NOBODY-THOUGHT-TO-LIST",
	})
	if len(got) != 1 || got[0] != "some-future-key-file-content" {
		t.Fatalf("unexpected: %v", got)
	}
	if strings.Contains(strings.Join(got, ","), "A-KEY-NOBODY") {
		t.Fatal("a value reached the line")
	}
}
