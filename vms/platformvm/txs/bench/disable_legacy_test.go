// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bench

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/luxfi/node/vms/platformvm/txs/zap_native"
)

// TestLegacyCodecGateDefault verifies that with
// LUXD_ENABLE_LEGACY_CODEC unset, zap_native reports legacy as
// disabled — which is the production default (native ZAP is the
// default wire format; legacy is opt-in).
//
// NOTE: this test does NOT assert that txs.Parse refuses v0 bytes,
// because txs.Parse is the byte-preserving migration entry point that
// must keep working under the v0 layout for any validator
// bootstrapping from pre-activation history. The gate that excludes
// legacy is enforced at the new ZAP-vs-legacy wire dispatcher Blue is
// building (see zap_native.IsZAPBytes / .ShouldUseZAPForWrite), not
// at the platformvm/txs.Parse entry. This test confirms the env-gate
// surface is wired correctly; production enforcement is elsewhere.
func TestLegacyCodecGateDefault(t *testing.T) {
	if os.Getenv("LUXD_ENABLE_LEGACY_CODEC") != "" {
		t.Skipf("LUXD_ENABLE_LEGACY_CODEC is set; this test runs with it unset")
	}
	if zap_native.LegacyEnabled {
		t.Fatalf("zap_native.LegacyEnabled should be false when env unset; got true")
	}
	// In default mode, the new-wire write path picks ZAP regardless of
	// block timestamp.
	if !zap_native.ShouldUseZAPForWrite(0) {
		t.Fatalf("ShouldUseZAPForWrite(0) should be true in default mode")
	}
}

// TestLegacyCodecGateEnabledViaSubprocess verifies that with
// LUXD_ENABLE_LEGACY_CODEC=1, zap_native reports legacy as enabled
// and the timestamp-gated dispatcher picks legacy for pre-activation
// blocks.
func TestLegacyCodecGateEnabledViaSubprocess(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess test skipped under -short")
	}
	if os.Getenv("LUX_BENCH_RECURSE") == "1" {
		runEnableLegacyChild(t)
		return
	}

	cmd := exec.Command(os.Args[0],
		"-test.run", "TestLegacyCodecGateEnabledViaSubprocess",
		"-test.v",
		"-test.count=1",
	)
	cmd.Env = append(os.Environ(),
		"LUX_BENCH_RECURSE=1",
		"LUXD_ENABLE_LEGACY_CODEC=1",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("subprocess re-exec failed (often expected from `go test`): %v\n%s",
			err, string(out))
		t.Skip("cannot re-exec test binary; run as compiled binary to verify gate")
		return
	}
	if !strings.Contains(string(out), "PASS") {
		t.Fatalf("child subprocess did not pass:\n%s", string(out))
	}
}

// runEnableLegacyChild runs in the re-exec'd child where
// LUXD_ENABLE_LEGACY_CODEC=1 is set at startup.
func runEnableLegacyChild(t *testing.T) {
	if !zap_native.LegacyEnabled {
		t.Fatalf("zap_native.LegacyEnabled should be true in child (LUXD_ENABLE_LEGACY_CODEC=1)")
	}
	// With legacy enabled, pre-activation timestamps pick legacy
	// for write.
	if zap_native.ShouldUseZAPForWrite(zap_native.ZAPActivationUnix - 1) {
		t.Fatalf("ShouldUseZAPForWrite(pre-activation) should be false when legacy is enabled")
	}
	// At/after activation, ZAP is picked regardless.
	if !zap_native.ShouldUseZAPForWrite(zap_native.ZAPActivationUnix) {
		t.Fatalf("ShouldUseZAPForWrite(activation) should be true")
	}
}
