//go:build tools
// +build tools

package main

import (
	"fmt"

	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/runtime"
)

func main() {
	var c *upgrade.Config = &upgrade.Config{}
	var _ runtime.NetworkUpgrades = c
	fmt.Println("Interface satisfied")
}
