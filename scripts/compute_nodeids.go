package main

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"

	"github.com/luxfi/ids"
)

func main() {
	// Compute NodeIDs for all 5 nodes
	for i := 1; i <= 5; i++ {
		certPath := fmt.Sprintf("/Users/z/.lux/node%d/staking/staker.crt", i)

		certPEM, err := os.ReadFile(certPath)
		if err != nil {
			fmt.Printf("Node%d: Error reading cert: %v\n", i, err)
			continue
		}

		block, _ := pem.Decode(certPEM)
		if block == nil {
			fmt.Printf("Node%d: Failed to decode PEM\n", i)
			continue
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			fmt.Printf("Node%d: Error parsing cert: %v\n", i, err)
			continue
		}

		// Use the ids package to compute NodeID
		nodeID := ids.NodeIDFromCert(&ids.Certificate{
			Raw:       cert.Raw,
			PublicKey: cert.PublicKey,
		})
		fmt.Printf("Node%d: %s\n", i, nodeID)
	}
}
