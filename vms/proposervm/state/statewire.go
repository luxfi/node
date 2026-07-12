// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

// Native-ZAP wire for the on-disk block store value (blockWrapper). The struct
// IS the wire: one fixed-shape zap object, no codec, no version prefix, no slot
// registry. Re-genesis means the on-disk format is free to change, so this
// layout is the canonical one.
//
// object (size 16): StatusInt u32 @0, Block bytes ptr @8.
// The stored block bytes are opaque — a self-delimiting zap block buffer
// re-parsed via block.ParseWithoutVerification, so the block ID is preserved
// with no re-encoding. Block bytes are copied out of the transient DB read
// buffer so a cached value never aliases it.

import "github.com/luxfi/zap"

const (
	bwStatus = 0
	bwBlock  = 8
	bwSize   = 16
)

func marshalBlockWrapper(bw *blockWrapper) []byte {
	b := zap.NewBuilder(zap.HeaderSize + bwSize + len(bw.Block))
	ob := b.StartObject(bwSize)
	ob.SetUint32(bwStatus, bw.StatusInt)
	ob.SetBytes(bwBlock, bw.Block)
	ob.FinishAsRoot()
	return b.Finish()
}

func parseBlockWrapper(bytes []byte, bw *blockWrapper) error {
	msg, err := zap.Parse(bytes)
	if err != nil {
		return err
	}
	root := msg.Root()
	bw.StatusInt = root.Uint32(bwStatus)
	if blk := root.Bytes(bwBlock); len(blk) > 0 {
		bw.Block = append([]byte(nil), blk...)
	}
	return nil
}
