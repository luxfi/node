// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"errors"
	"os"
)

// ZAPActivationUnix is the unix timestamp at which P-chain wire format
// switches from legacy linearcodec (CodecVersionV0/V1) to native ZAP. The
// switch governs the WRITE path: post-activation blocks MUST be ZAP-encoded.
// Validator coordination is documented in LP-023.
//
// Value: 2026-07-01 00:00 UTC = 1782604800.
const ZAPActivationUnix uint64 = 1782604800

// LegacyEnabled is true when the operator has set LUXD_ENABLE_LEGACY_CODEC=1.
//
// **Native ZAP is the default.** The codec.Codec{Marshal,Unmarshal} interface
// is gone from the platformvm hot path. New deployments, fresh syncs, and
// post-activation production binaries run ZAP-only by default — no legacy
// code in the read or write path.
//
// Operators MUST set LUXD_ENABLE_LEGACY_CODEC=1 if they need to read
// pre-activation historical blocks from disk (e.g. validators bootstrapping
// from a long-history snapshot that includes pre-2026-07-01 P-chain state).
// Without the flag, legacy-encoded bytes return ErrLegacyCodecDisabled.
//
// Once a validator has synced past the activation height, the flag can be
// dropped — all subsequent reads are ZAP.
var LegacyEnabled = os.Getenv("LUXD_ENABLE_LEGACY_CODEC") == "1"

// ErrLegacyCodecDisabled is returned by legacy-path parsers when the operator
// has not enabled legacy codec support via the LUXD_ENABLE_LEGACY_CODEC env var.
// Native ZAP is the default; legacy is opt-in.
var ErrLegacyCodecDisabled = errors.New("zap_native: legacy codec not enabled (set LUXD_ENABLE_LEGACY_CODEC=1 to read pre-activation blocks)")

// IsZAPBytes reports whether the byte buffer is a ZAP-encoded message
// (recognised by the 4-byte "ZAP\x00" magic). Cheap O(1) check.
//
// Callers use this to discriminate ZAP-encoded txs/blocks from legacy
// linearcodec-encoded ones during the cross-activation read window (which
// only happens when LUXD_ENABLE_LEGACY_CODEC=1).
func IsZAPBytes(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	return b[0] == 'Z' && b[1] == 'A' && b[2] == 'P' && b[3] == 0
}

// ShouldUseZAPForWrite reports whether new outgoing txs/blocks should be
// encoded as ZAP.
//
// Default behavior — native ZAP always. The block timestamp gate exists ONLY
// to maintain consensus with pre-activation peers during the cutover window;
// once activation passes, this function returns true unconditionally.
//
// When LUXD_ENABLE_LEGACY_CODEC=1, pre-activation timestamps still emit
// legacy linearcodec to preserve back-compat with un-upgraded peers.
func ShouldUseZAPForWrite(blockTimestamp uint64) bool {
	if !LegacyEnabled {
		return true // native ZAP default
	}
	return blockTimestamp >= ZAPActivationUnix
}
