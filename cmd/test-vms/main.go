// Copyright (C) 2019-2025, Lux Partners Limited All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"

	_ "github.com/luxfi/node/vms/avm"
	_ "github.com/luxfi/node/vms/bvm"
	_ "github.com/luxfi/node/vms/zvm"
)

func main() {
	fmt.Println("✅ AVM (Attestation VM) - compiled successfully")
	fmt.Println("✅ BVM (Bridge VM) - compiled successfully")
	fmt.Println("✅ ZVM (Zero-Knowledge VM) - compiled successfully")
	fmt.Println("\nAll 3 VMs are ready for integration!")
}
