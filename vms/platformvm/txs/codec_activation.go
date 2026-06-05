// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

// ZAPCodecActivationTimestamp is the unix timestamp at which the
// canonical write codec for the P-chain transitions from
// linearcodec (V1, big-endian) to zapcodec (V2, little-endian).
//
// Value: 1782864000 = 2026-07-01 00:00:00 UTC.
//
// Why 2026-07-01:
//
//   - The historical Quasar activation milestone (Dec 25 2025 16:20
//     PST) is retro-impossible for a wire-format flip; the cluster
//     has been running and producing V1-encoded blocks since then.
//   - 2026-07-01 aligns with the existing post-Quasar phase-2
//     precompile activation calendar (see the network of blockchains
//     architecture memo in MEMORY.md). Bundling wire-codec flips
//     with precompile bundles reduces the number of forward-dated
//     activations operators must track.
//   - Far enough out that every validator binary in the field before
//     activation will recognise V2 as a registered codec (this
//     constant ships in v1.28.x+ and the activation gives operators
//     a multi-week soak window).
//
// Read path is timestamp-blind: any binary with this constant baked
// in unmarshals both V1 and V2 bytes via codec.Manager's wire-prefix
// dispatch. The timestamp gates only the WRITE path — which version
// the mempool/block-builder/signer emits.
//
// Coordination protocol: changing this constant requires every
// validator to ship the new binary BEFORE the new timestamp ticks.
// Otherwise validators on the old binary will reject V2-prefixed
// blocks produced by the new binary (codec.Manager returns
// ErrUnknownVersion) and the chain halts. A long forward-dated
// activation gives operators time to upgrade without coordination
// drama.
const ZAPCodecActivationTimestamp uint64 = 1782864000
