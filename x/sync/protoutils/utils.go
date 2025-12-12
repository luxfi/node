// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package protoutils

import (
	"github.com/luxfi/node/utils/maybe"

	pb "github.com/luxfi/node/proto/pb/sync"
)

func MaybeToProto(m maybe.Maybe[[]byte]) *pb.MaybeBytes {
	if m.IsNothing() {
		return nil
	}
	return &pb.MaybeBytes{
		Value: m.Value(),
	}
}

func ProtoToMaybe(mb *pb.MaybeBytes) maybe.Maybe[[]byte] {
	if mb == nil {
		return maybe.Nothing[[]byte]()
	}
	return maybe.Some(mb.Value)
}
