package main

import (
	"encoding/hex"
	"fmt"

	"github.com/luxfi/crypto/cb58"
)

func main() {
	key, _ := hex.DecodeString("95a452d218b45566d386e2053adfd05810202fe818ee7eb14a902f75b2e7d043")
	encoded, _ := cb58.Encode(key)
	fmt.Println(encoded)
}
