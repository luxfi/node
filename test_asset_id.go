// +build ignore

package main

import (
    "fmt"
    "github.com/luxfi/node/ids"
)

func main() {
    id, err := ids.FromString("d1Rdokz7Vq8H5aczkwgkiPCCa6JME7yT2xpqgWTfFKWYVsGbG")
    if err != nil {
        panic(err)
    }
    
    bytes := id[:]
    fmt.Printf("LUX Asset ID bytes:\n")
    for i := 0; i < len(bytes); i += 8 {
        fmt.Printf("0x%02x, 0x%02x, 0x%02x, 0x%02x, 0x%02x, 0x%02x, 0x%02x, 0x%02x,\n",
            bytes[i], bytes[i+1], bytes[i+2], bytes[i+3], 
            bytes[i+4], bytes[i+5], bytes[i+6], bytes[i+7])
    }
}