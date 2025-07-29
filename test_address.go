// +build ignore

package main

import (
    "fmt"
    "github.com/luxfi/ids"
    "github.com/luxfi/node/utils/constants"
    "github.com/luxfi/node/utils/formatting/address"
)

func main() {
    addr := ids.ShortID{
        0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
        0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
        0x44, 0x55, 0x66, 0x77,
    }
    
    formatted, err := address.Format("P", constants.GetHRP(constants.MainnetID), addr[:])
    if err != nil {
        panic(err)
    }
    
    fmt.Printf("Address: %s\n", formatted)
}