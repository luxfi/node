// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"

	"github.com/luxfi/ids"
)

func main() {
	evmID := ids.ID{'e', 'v', 'm'}
	fmt.Printf("EVMID bytes: %v\n", evmID)
	fmt.Printf("EVMID string: %s\n", evmID.String())
}
