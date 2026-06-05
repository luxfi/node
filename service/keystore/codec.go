// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keystore

import (
	"encoding/binary"
	"errors"
	"math"

	"github.com/luxfi/node/utils/password"
)

// On-disk encoding of keystore user records. Big-endian wire layout.
//
// Hash bytes (48 bytes, fixed):
//
//	bytes 0..31  : Hash.Password (argon2id digest)
//	bytes 32..47 : Hash.Salt
//
// user bytes:
//
//	bytes 0..47  : Hash (as above)
//	bytes 48..51 : len(Data)                       (uint32 BE)
//	then for each kvPair:
//	  uint32 BE: len(Key)
//	  len(Key) bytes: Key
//	  uint32 BE: len(Value)
//	  len(Value) bytes: Value
//
// No codec version prefix. maxKeystoreBlob bounds total decoded size to
// the historic 16 MiB cap (papers/oom-audit-2026-04-12.tex F-4).

const (
	// maxKeystoreBlob caps the size of any single keystore payload.
	// Real user keystores are well under 1 MiB; 16 MiB is generous
	// while bounding server-side allocation to a non-pathological size.
	maxKeystoreBlob = 16 * 1024 * 1024

	hashLen = 32 + 16
)

var (
	errKeystoreBlobOversize  = errors.New("keystore: blob exceeds 16 MiB cap")
	errKeystoreHashShort     = errors.New("keystore: hash bytes too short")
	errKeystoreUserShort     = errors.New("keystore: user bytes too short")
	errKeystoreTrailing      = errors.New("keystore: trailing bytes after decode")
	errKeystoreEntryOversize = errors.New("keystore: kv entry exceeds blob")
)

func marshalHash(h *password.Hash) ([]byte, error) {
	out := make([]byte, hashLen)
	copy(out[:32], h.Password[:])
	copy(out[32:], h.Salt[:])
	return out, nil
}

func unmarshalHash(b []byte, h *password.Hash) error {
	if len(b) < hashLen {
		return errKeystoreHashShort
	}
	if len(b) > maxKeystoreBlob {
		return errKeystoreBlobOversize
	}
	copy(h.Password[:], b[:32])
	copy(h.Salt[:], b[32:hashLen])
	if len(b) > hashLen {
		return errKeystoreTrailing
	}
	return nil
}

func marshalUser(u *user) ([]byte, error) {
	// Size estimate.
	total := uint64(hashLen + 4)
	for _, kv := range u.Data {
		total += 4 + uint64(len(kv.Key)) + 4 + uint64(len(kv.Value))
	}
	if total > maxKeystoreBlob {
		return nil, errKeystoreBlobOversize
	}
	out := make([]byte, 0, total)
	out = append(out, u.Hash.Password[:]...)
	out = append(out, u.Hash.Salt[:]...)
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(u.Data)))
	out = append(out, lenBuf[:]...)
	for _, kv := range u.Data {
		if len(kv.Key) > math.MaxUint32 || len(kv.Value) > math.MaxUint32 {
			return nil, errKeystoreEntryOversize
		}
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(kv.Key)))
		out = append(out, lenBuf[:]...)
		out = append(out, kv.Key...)
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(kv.Value)))
		out = append(out, lenBuf[:]...)
		out = append(out, kv.Value...)
	}
	return out, nil
}

func unmarshalUser(b []byte, u *user) error {
	if len(b) > maxKeystoreBlob {
		return errKeystoreBlobOversize
	}
	if len(b) < hashLen+4 {
		return errKeystoreUserShort
	}
	copy(u.Hash.Password[:], b[:32])
	copy(u.Hash.Salt[:], b[32:hashLen])
	off := hashLen
	count := binary.BigEndian.Uint32(b[off : off+4])
	off += 4
	// Bound the slice growth by the remaining payload — each entry
	// is at least 8 bytes (two length prefixes), so count cannot
	// exceed (len(b)-off)/8 without underflowing.
	maxEntries := uint64(len(b)-off) / 8
	if uint64(count) > maxEntries {
		return errKeystoreEntryOversize
	}
	u.Data = make([]kvPair, count)
	for i := range u.Data {
		if off+4 > len(b) {
			return errKeystoreUserShort
		}
		klen := binary.BigEndian.Uint32(b[off : off+4])
		off += 4
		if uint64(off)+uint64(klen) > uint64(len(b)) {
			return errKeystoreEntryOversize
		}
		u.Data[i].Key = make([]byte, klen)
		copy(u.Data[i].Key, b[off:off+int(klen)])
		off += int(klen)
		if off+4 > len(b) {
			return errKeystoreUserShort
		}
		vlen := binary.BigEndian.Uint32(b[off : off+4])
		off += 4
		if uint64(off)+uint64(vlen) > uint64(len(b)) {
			return errKeystoreEntryOversize
		}
		u.Data[i].Value = make([]byte, vlen)
		copy(u.Data[i].Value, b[off:off+int(vlen)])
		off += int(vlen)
	}
	if off != len(b) {
		return errKeystoreTrailing
	}
	return nil
}
