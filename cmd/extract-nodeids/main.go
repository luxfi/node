package main

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"github.com/luxfi/ids"
)

func main() {
	homeDir, _ := os.UserHomeDir()
	for i := 1; i <= 5; i++ {
		certPath := filepath.Join(homeDir, ".lux", fmt.Sprintf("node%d", i), "staking", "staker.crt")
		certPEM, err := os.ReadFile(certPath)
		if err != nil {
			fmt.Printf("Error reading cert %d: %v\n", i, err)
			continue
		}
		block, _ := pem.Decode(certPEM)
		if block == nil {
			fmt.Printf("Failed to decode PEM for node %d\n", i)
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			fmt.Printf("Error parsing cert %d: %v\n", i, err)
			continue
		}
		nodeID := ids.NodeIDFromCert(&ids.Certificate{
			Raw:       cert.Raw,
			PublicKey: cert.PublicKey,
		})
		fmt.Printf("Node%d: NodeID-%s\n", i, nodeID.String())
	}
}
