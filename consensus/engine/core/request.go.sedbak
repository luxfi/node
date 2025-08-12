// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"fmt"

	"github.com/luxfi/ids"
)

type Request struct {
	NodeID    ids.NodeID
	RequestID uint32
}

func (r Request) MarshalText() ([]byte, error) {
	return []byte(fmt.Sprintf("%s:%d", r.NodeID, r.RequestID)), nil
}
