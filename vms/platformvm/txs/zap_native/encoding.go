// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import "encoding/binary"

// putU32 / putU64 are little-endian stride writers used by primitive list
// constructors. Kept here to avoid each primitive file pulling encoding/binary
// directly.

func putU32(b []byte, v uint32) { binary.LittleEndian.PutUint32(b, v) }
func putU64(b []byte, v uint64) { binary.LittleEndian.PutUint64(b, v) }
