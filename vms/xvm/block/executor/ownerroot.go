// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"bytes"
	"slices"

	"github.com/luxfi/crypto/hash"
	"github.com/luxfi/ids"
	"github.com/luxfi/utxo/bls12381fx"
	"github.com/luxfi/utxo/ed25519fx"
	"github.com/luxfi/utxo/mldsafx"
	"github.com/luxfi/utxo/nftfx"
	"github.com/luxfi/utxo/propertyfx"
	"github.com/luxfi/utxo/schnorrfx"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/utxo/secp256r1fx"
	"github.com/luxfi/utxo/slhdsafx"
	"github.com/luxfi/vm/components/verify"
)

// owners is the resolved owner condition of an xvm output, reduced to the two
// values the execution_root binds: the spend Threshold (the N-of-M parameter)
// and the canonical, ascending-sorted owner key set. For an address-keyed
// output (every secp256k1fx-family / nft / property output) the keys are the
// 20-byte secp256k1 addresses; for a bls12381fx attestation output the keys are
// the committee public keys. Locktime is carried separately (the leaf binds it
// in its own field).
type owners struct {
	threshold uint32
	locktime  uint64
	// keys is the owner key set in canonical ascending byte order: secp256k1
	// addresses are already sorted-and-unique by OutputOwners.Verify; bls
	// committee pubkeys are consensus-sorted lexicographically.
	keys [][]byte
}

// OwnerRoot is the CANONICAL Go definition of the execution_root UTXO leaf's
// owner_root field. It is the authority the GPU state-snapshot producer MUST
// match (the GPU/CPU kernels treat owner_root as a host-supplied input —
// xvm_gpu_layout.hpp documents its intent as "keccak over sorted owner address
// set + threshold" but does not derive it; this is where that derivation is
// pinned).
//
// owner_root = keccak256(
//
//	threshold_u32_le ‖ key_count_u32_le ‖ key[0] ‖ key[1] ‖ … ‖ key[k-1] )
//
// where:
//   - threshold is the output's N-of-M spend threshold (binds the M-of-N
//     policy, per the layout's "+ threshold");
//   - key_count is the number of owner keys, little-endian u32 — a length
//     prefix so two owner sets that differ only by a key-set/threshold framing
//     can never collide (no ambiguous concatenation);
//   - each key is an owner key in canonical ascending byte order: for an
//     address-keyed output the 20-byte secp256k1 address; for a bls attestation
//     output the committee public key. The order is the same sorted-and-unique
//     order the output's own Verify enforces, so the preimage is deterministic
//     and independent of the order keys happened to be listed in.
//
// The keccak256 is Ethereum Keccak-256 (the same hash xvmroot/the GPU use for
// every leaf preimage), keeping owner_root in the one hash family the whole
// execution_root is built from. Integers are little-endian to match the
// kernel's absorb_u32.
//
// Locktime is NOT folded into owner_root: the UTXOLeaf binds Locktime in its own
// dedicated field (matching the GPU UTXO struct, which carries locktime and
// owner_root separately), so binding it here too would be redundant. Threshold
// is bound in BOTH owner_root and the leaf's Threshold field, exactly as the GPU
// struct carries threshold both inside the host-computed owner_root and as a
// standalone field.
func OwnerRoot(o owners) [32]byte {
	// 4 (threshold) + 4 (count) + sum(len(key)). Addresses are 20 bytes; bls
	// pubkeys are larger but fixed per output — size the buffer exactly.
	n := 8
	for _, k := range o.keys {
		n += len(k)
	}
	b := make([]byte, 0, n)
	b = le32(b, o.threshold)
	b = le32(b, uint32(len(o.keys)))
	for _, k := range o.keys {
		b = append(b, k...)
	}
	return hash.ComputeKeccak256Array(b)
}

// le32 appends v as little-endian u32 — the same encoding xvmroot.le32 / the
// kernel's absorb_u32 use. Kept local to the owner-root preimage builder; the
// xvmroot copy is unexported, and duplicating four bytes of shifting here is
// cheaper than widening that package's API for one caller.
func le32(b []byte, v uint32) []byte {
	return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

// extractOwners resolves an xvm output's owner condition into the canonical
// {threshold, locktime, sorted key set} the execution_root binds. ok is false
// only for an output whose owner model the execution_root projection does not
// yet define a canonical owner_root for (flagged to the caller rather than
// hashed under an invented rule).
//
// Every value/authority output xvm can hold reduces to one of two owner models:
//
//   - secp256k1fx.OutputOwners (addresses + threshold + locktime): the
//     secp256k1fx / secp256r1fx / ed25519fx / schnorrfx / mldsafx / slhdsafx
//     transfer and mint outputs, the nftfx / propertyfx outputs, and a bare
//     *OutputOwners all carry it. Transfer outputs expose it via the canonical
//     fx.Owned accessor (Owners() interface{}); the rest embed it, so we read
//     the embedded value by concrete type. The key set is the 20-byte address
//     list, already sorted-and-unique by OutputOwners.Verify.
//
//   - bls12381fx.AttestationOutput (committee pubkeys + threshold): a distinct
//     owner model with no addresses. Its key set is the consensus-sorted
//     committee pubkeys; locktime is 0 (attestations have no locktime).
func extractOwners(out verify.State) (owners, bool) {
	// bls attestation outputs use a committee-pubkey owner model, not address
	// OutputOwners — handle them first.
	if a, ok := out.(*bls12381fx.AttestationOutput); ok {
		return owners{threshold: a.Threshold, keys: sortedCopy(a.PubKeys)}, true
	}

	// Canonical accessor: every fx transfer output returns its embedded
	// *<fx>.OutputOwners from Owners() (the fx.Owned contract). The mint / nft /
	// property outputs do not expose Owners(), so reach their embedded owners by
	// concrete type. In both cases the value handed to ownersFromAny is one of
	// the six *<fx>.OutputOwners shapes, which it reduces uniformly.
	if owned, ok := out.(interface{ Owners() interface{} }); ok {
		if o, ok := ownersFromAny(owned.Owners()); ok {
			return o, true
		}
	}

	switch o := out.(type) {
	case *secp256k1fx.MintOutput:
		return ownersFromOutputOwners(o.Threshold, o.Locktime, o.Addrs), true
	case *secp256r1fx.MintOutput:
		return ownersFromOutputOwners(o.Threshold, o.Locktime, o.Addrs), true
	case *ed25519fx.MintOutput:
		return ownersFromOutputOwners(o.Threshold, o.Locktime, o.Addrs), true
	case *schnorrfx.MintOutput:
		return ownersFromOutputOwners(o.Threshold, o.Locktime, o.Addrs), true
	case *mldsafx.MintOutput:
		return ownersFromByteKeys(o.Threshold, o.Locktime, o.Addrs), true
	case *slhdsafx.MintOutput:
		return ownersFromByteKeys(o.Threshold, o.Locktime, o.Addrs), true
	case *nftfx.TransferOutput:
		return ownersFromOutputOwners(o.Threshold, o.Locktime, o.Addrs), true
	case *nftfx.MintOutput:
		return ownersFromOutputOwners(o.Threshold, o.Locktime, o.Addrs), true
	case *propertyfx.MintOutput:
		return ownersFromOutputOwners(o.Threshold, o.Locktime, o.Addrs), true
	case *propertyfx.OwnedOutput:
		return ownersFromOutputOwners(o.Threshold, o.Locktime, o.Addrs), true
	default:
		return owners{}, false
	}
}

// ownersFromAny reduces the value returned by an output's Owners() accessor —
// one of the six *<fx>.OutputOwners shapes — to the canonical owner condition.
// The four secp/ed/schnorr-family owners key on 20-byte addresses
// (Addrs []ids.ShortID); the two PQ families (ml-dsa, slh-dsa) key on full
// public keys (Addrs [][]byte). Both reduce to a threshold + locktime + sorted
// key set.
func ownersFromAny(v interface{}) (owners, bool) {
	switch o := v.(type) {
	case *secp256k1fx.OutputOwners:
		return ownersFromOutputOwners(o.Threshold, o.Locktime, o.Addrs), true
	case *secp256r1fx.OutputOwners:
		return ownersFromOutputOwners(o.Threshold, o.Locktime, o.Addrs), true
	case *ed25519fx.OutputOwners:
		return ownersFromOutputOwners(o.Threshold, o.Locktime, o.Addrs), true
	case *schnorrfx.OutputOwners:
		return ownersFromOutputOwners(o.Threshold, o.Locktime, o.Addrs), true
	case *mldsafx.OutputOwners:
		return ownersFromByteKeys(o.Threshold, o.Locktime, o.Addrs), true
	case *slhdsafx.OutputOwners:
		return ownersFromByteKeys(o.Threshold, o.Locktime, o.Addrs), true
	default:
		return owners{}, false
	}
}

// ownersFromOutputOwners reduces an address-keyed owner condition (20-byte
// ids.ShortID addresses) to canonical owners. The addresses are sorted into
// canonical ascending byte order — the same sorted-and-unique order
// OutputOwners.Verify enforces on a valid output, made explicit so owner_root is
// a deterministic function of the address SET, independent of the order the
// addresses happened to be listed in.
func ownersFromOutputOwners(threshold uint32, locktime uint64, addrs []ids.ShortID) owners {
	keys := make([][]byte, len(addrs))
	for i := range addrs {
		keys[i] = addrs[i].Bytes()
	}
	slices.SortFunc(keys, bytes.Compare)
	return owners{threshold: threshold, locktime: locktime, keys: keys}
}

// ownersFromByteKeys reduces a public-key-keyed owner condition (the PQ fxs key
// on full public keys, Addrs [][]byte) to canonical owners. The keys are sorted
// into canonical ascending order so owner_root is independent of listing order
// (the PQ OutputOwners.Verify enforces sorted-unique keys, so this is the same
// order, made explicit).
func ownersFromByteKeys(threshold uint32, locktime uint64, keys [][]byte) owners {
	return owners{threshold: threshold, locktime: locktime, keys: sortedCopy(keys)}
}

// sortedCopy returns a copy of keys in canonical ascending lexicographic byte
// order — the same byteLess order bls12381fx.AttestationOutput.Verify enforces
// on its committee pubkeys. The copy makes the canonical order explicit and
// defensive without mutating the caller's slice.
func sortedCopy(keys [][]byte) [][]byte {
	out := make([][]byte, len(keys))
	copy(out, keys)
	slices.SortFunc(out, bytes.Compare)
	return out
}
