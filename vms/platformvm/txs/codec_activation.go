// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

// ZAPCodecActivationTimestamp is the unix timestamp at which the
// canonical write codec for the P-chain transitions from V1 to V2.
//
// Value: 0 — ZAP-native is mandatory from genesis (LP-023). Aligned
// with proto/zap_native/codec_select.go: ZAPActivationUnix=0. There
// is no pre-activation regime in this binary; every emitted block
// carries the V2 wire prefix.
//
// V1 remains a registered READ slot for nominal historical
// compatibility (the slot map mechanism that would otherwise serve a
// pre-activation network). In this binary V1 and V2 use identical
// LE wire encoding and identical slot maps, so the version byte is
// the only thing that distinguishes them on the wire.
//
// Read path is timestamp-blind: any binary with this constant baked
// in unmarshals both V1 and V2 bytes via the multi-version codec's
// wire-prefix dispatch. The timestamp gates only the WRITE path —
// which version the mempool/block-builder/signer emits.
const ZAPCodecActivationTimestamp uint64 = 0
