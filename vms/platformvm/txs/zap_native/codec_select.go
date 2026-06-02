// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"errors"
	"os"
)

// ZAPActivationUnix is the unix timestamp at which P-chain wire format
// switches from linearcodec (CodecVersionV0/V1) to native ZAP. Pre-activation
// blocks are accepted in their existing linearcodec encoding via the legacy
// codec.Manager path in ~/work/lux/node/vms/platformvm/txs/codec.go. Post-
// activation blocks MUST be ZAP. Validator coordination is documented in
// LP-023.
//
// Value chosen: 2026-07-01 00:00 UTC = 1782604800.
//
// This is ~30 days forward of the migration land date (2026-06-02). Sufficient
// notice for validator upgrades.
const ZAPActivationUnix uint64 = 1782604800

// DisableLegacy is true when the operator has set LUXD_DISABLE_LEGACY_CODEC=1.
// When set:
//   - The legacy linearcodec read path returns ErrLegacyCodecDisabled.
//   - All write paths use ZAP regardless of block timestamp.
//
// Used for:
//   - Benchmarking pure ZAP perf without legacy code in the binary's hot path.
//   - Future post-activation production runs where backward compat is no
//     longer required and the operator wants to drop the codec.Manager surface
//     for a smaller, simpler, faster binary.
var DisableLegacy = os.Getenv("LUXD_DISABLE_LEGACY_CODEC") == "1"

// ErrLegacyCodecDisabled is returned by legacy-path parsers when the operator
// has disabled legacy codec support via the LUXD_DISABLE_LEGACY_CODEC env var.
var ErrLegacyCodecDisabled = errors.New("zap_native: legacy codec disabled (LUXD_DISABLE_LEGACY_CODEC=1)")

// IsZAPBytes reports whether the byte buffer is a ZAP-encoded message
// (recognised by the 4-byte "ZAP\x00" magic). Cheap O(1) check.
//
// Callers use this to discriminate ZAP-encoded txs/blocks from legacy
// linearcodec-encoded ones during the cross-activation read window.
func IsZAPBytes(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	return b[0] == 'Z' && b[1] == 'A' && b[2] == 'P' && b[3] == 0
}

// ShouldUseZAPForWrite reports whether new outgoing txs/blocks should be
// encoded as ZAP. True iff:
//   - blockTimestamp >= ZAPActivationUnix (we're post-activation), OR
//   - LUXD_DISABLE_LEGACY_CODEC=1 (operator opt-in)
func ShouldUseZAPForWrite(blockTimestamp uint64) bool {
	return blockTimestamp >= ZAPActivationUnix || DisableLegacy
}
