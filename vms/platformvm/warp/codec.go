// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"errors"

	"github.com/luxfi/node/vms/pcodecs"
)

const CodecVersion = 0

var Codec pcodecs.Manager

func init() {
	Codec = pcodecs.NewMaxIntManager()
	lc := pcodecs.NewLinearCodec()

	// CRITICAL: RegisterType order is the on-chain wire format. linearcodec
	// assigns a monotonically increasing typeID per call. Reordering ANY of
	// these lines is a hard fork. New types must be appended ONLY.
	//
	//   typeID 0x00 → BitSetSignature
	//   typeID 0x01 → CoronaSignature
	//   typeID 0x02 → EncryptedWarpPayload
	//   typeID 0x03 → HybridBLSCoronaSignature  (renamed from HybridBLSRTSignature)
	//   typeID 0x04 → TeleportMessage
	//   typeID 0x05 → TeleportTransferPayload
	//   typeID 0x06 → TeleportAttestPayload
	err := errors.Join(
		// Warp 1.0: Classical BLS signatures
		lc.RegisterType(&BitSetSignature{}),
		// Warp 1.5: Quantum-safe signatures
		lc.RegisterType(&CoronaSignature{}),          // Recommended: Corona-only (LWE-based threshold)
		lc.RegisterType(&EncryptedWarpPayload{}),     // ML-KEM + AES-256-GCM encryption
		lc.RegisterType(&HybridBLSCoronaSignature{}), // Deprecated: BLS+Corona hybrid
		// Teleport: Cross-chain bridging protocol
		lc.RegisterType(&TeleportMessage{}),         // High-level bridge message wrapper
		lc.RegisterType(&TeleportTransferPayload{}), // Asset transfer payload
		lc.RegisterType(&TeleportAttestPayload{}),   // Attestation payload
		Codec.RegisterCodec(CodecVersion, lc),
	)
	if err != nil {
		panic(err)
	}
}
