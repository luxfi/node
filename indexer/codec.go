// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package indexer

import (
	"encoding/binary"
	"errors"

	"github.com/luxfi/ids"
)

// On-disk encoding of an indexed Container. Big-endian wire layout:
//
//	bytes 0..31  : Container.ID            (ids.IDLen)
//	bytes 32..35 : len(Container.Bytes)    (uint32 BE)
//	bytes 36..N  : Container.Bytes         (variable)
//	bytes N..N+8 : Container.Timestamp     (int64 BE, two's complement)
//
// No codec version prefix — the indexer DB lives inside the node, so a
// forward-only schema is fine. Older versions stored a 2-byte codec
// version header; that format is no longer accepted (hard cut from
// luxfi/codec rip — Wave 1D).

var (
	errContainerTooShort = errors.New("indexer: encoded container shorter than header")
	errContainerBytesLen = errors.New("indexer: encoded container length exceeds payload")
)

// marshalContainer encodes c to its on-disk representation.
func marshalContainer(c Container) ([]byte, error) {
	out := make([]byte, 0, ids.IDLen+4+len(c.Bytes)+8)
	out = append(out, c.ID[:]...)
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(c.Bytes)))
	out = append(out, lenBuf[:]...)
	out = append(out, c.Bytes...)
	var tsBuf [8]byte
	binary.BigEndian.PutUint64(tsBuf[:], uint64(c.Timestamp))
	out = append(out, tsBuf[:]...)
	return out, nil
}

// unmarshalContainer decodes b into a Container.
func unmarshalContainer(b []byte) (Container, error) {
	const headerLen = ids.IDLen + 4
	if len(b) < headerLen+8 {
		return Container{}, errContainerTooShort
	}
	var c Container
	copy(c.ID[:], b[:ids.IDLen])
	bytesLen := binary.BigEndian.Uint32(b[ids.IDLen : ids.IDLen+4])
	if uint64(len(b)) < uint64(headerLen)+uint64(bytesLen)+8 {
		return Container{}, errContainerBytesLen
	}
	payloadStart := headerLen
	payloadEnd := payloadStart + int(bytesLen)
	c.Bytes = make([]byte, bytesLen)
	copy(c.Bytes, b[payloadStart:payloadEnd])
	c.Timestamp = int64(binary.BigEndian.Uint64(b[payloadEnd : payloadEnd+8]))
	return c, nil
}
