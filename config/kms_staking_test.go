// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"crypto/rand"
	"strings"
	"testing"

	"github.com/luxfi/ids"
	"github.com/luxfi/keys"
	node "github.com/luxfi/node/config/node"
	"github.com/spf13/viper"
)

const kmsTestMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

// applyStakingKMS must install a WORKING strict-PQ signing key. It wipes the
// resolved bundle on return (defer id.Wipe()); if the installed *mldsa key
// aliased the bundle bytes, that wipe would zero the node's live signing key.
// This test signs with the installed key AFTER applyStakingKMS returns — it
// fails (empty/garbage signature) if the aliasing regression is reintroduced.
func TestApplyStakingKMS_InstalledKeySurvivesBundleWipe(t *testing.T) {
	t.Setenv("MNEMONIC", kmsTestMnemonic) // env short-circuit: no live KMS dialed

	v := viper.New()
	v.Set(StakingKMSEndpointKey, "kms:9999")
	v.Set(StakingKMSEnvKey, "test")
	v.Set(StakingKMSMnemonicPathKey, "providers/lux/deploy-mnemonic")
	v.Set(StakingKMSValidatorIndexKey, 2)
	v.Set(StakingKMSStrictPQKey, true)
	v.Set(StakingKMSAllowEnvMnemonicKey, true) // dev seam: honor MNEMONIC env (no live KMS dialed)

	var cfg node.StakingConfig
	if err := applyStakingKMS(v, &cfg); err != nil {
		t.Fatalf("applyStakingKMS: %v", err)
	}

	// Classical materials installed.
	if cfg.StakingTLSCert.Leaf == nil {
		t.Fatal("StakingTLSCert.Leaf is nil")
	}
	if cfg.StakingSigningKey == nil {
		t.Fatal("StakingSigningKey (BLS) is nil")
	}

	// Strict-PQ installed and the node flips to a strict-PQ NodeID.
	if !cfg.IsStrictPQ() {
		t.Fatal("expected IsStrictPQ() true after --staking-kms-strict-pq")
	}
	if cfg.StakingMLDSA == nil {
		t.Fatal("StakingMLDSA is nil")
	}

	// THE regression guard: sign with the installed key (post-wipe) and verify.
	msg := []byte("post-wipe signing check")
	sig, err := cfg.StakingMLDSA.Sign(rand.Reader, msg, nil)
	if err != nil {
		t.Fatalf("installed ML-DSA key failed to sign (aliased-then-wiped?): %v", err)
	}
	if !cfg.StakingMLDSA.PublicKey.VerifySignature(msg, sig) {
		t.Fatal("signature from installed ML-DSA key did not verify (key was corrupted by bundle wipe)")
	}

	// NodeID matches the independent strict-PQ derivation at the same index.
	wantPQ, err := keys.DeriveValidatorPQ(kmsTestMnemonic, 2)
	if err != nil {
		t.Fatalf("reference derive: %v", err)
	}
	wantNodeID, err := wantPQ.StrictPQNodeID(ids.Empty)
	if err != nil {
		t.Fatalf("reference NodeID: %v", err)
	}
	gotNodeID, err := cfg.DeriveNodeID(ids.Empty)
	if err != nil {
		t.Fatalf("DeriveNodeID: %v", err)
	}
	if gotNodeID != wantNodeID {
		t.Fatalf("installed NodeID %s != expected %s", gotNodeID, wantNodeID)
	}
}

// M1: without --staking-kms-strict-pq, DERIVE mode is REFUSED — classical DERIVE
// churns the NodeID every boot. applyStakingKMS surfaces the refusal.
func TestApplyStakingKMS_ClassicalDeriveRefused(t *testing.T) {
	t.Setenv("MNEMONIC", kmsTestMnemonic)
	v := viper.New()
	v.Set(StakingKMSEndpointKey, "kms:9999")
	v.Set(StakingKMSEnvKey, "test")
	v.Set(StakingKMSMnemonicPathKey, "providers/lux/deploy-mnemonic")
	v.Set(StakingKMSAllowEnvMnemonicKey, true) // env seam allowed; classical DERIVE still refused

	var cfg node.StakingConfig
	if err := applyStakingKMS(v, &cfg); err == nil {
		t.Fatal("expected classical DERIVE to be refused")
	}
}

// L2: any staking-kms-* flag set WITHOUT --staking-kms-endpoint is refused —
// the node must not silently fall through to local keys when the operator
// clearly intended KMS.
func TestGetStakingConfig_KMSFlagWithoutEndpointRefused(t *testing.T) {
	v := viper.New()
	v.Set(SybilProtectionEnabledKey, true) // pass the sybil gates to reach the L2 gate
	// A KMS mode flag is set, but the endpoint is NOT.
	v.Set(StakingKMSMnemonicPathKey, "providers/lux/deploy-mnemonic")

	if _, err := getStakingConfig(v, 12345); err == nil {
		t.Fatal("expected refusal: staking-kms-* flag set without --staking-kms-endpoint")
	} else if !strings.Contains(err.Error(), StakingKMSEndpointKey) {
		t.Fatalf("error should name the missing endpoint flag, got: %v", err)
	}
}
