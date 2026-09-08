// pqkeygen provisions the strict-PQ staking keypairs a local luxd needs:
// ML-DSA-65 (FIPS 204) staking key + ML-KEM-768 (FIPS 203) handshake key.
// Writes PEM blocks with the exact types the node's config loader expects.
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"github.com/luxfi/crypto/mldsa"
	"github.com/luxfi/crypto/mlkem"
)

func writePEM(path, typ string, der []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var b bytes.Buffer
	if err := pem.Encode(&b, &pem.Block{Type: typ, Bytes: der}); err != nil {
		return err
	}
	return os.WriteFile(path, b.Bytes(), 0o600)
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: pqkeygen <staking-dir>")
		os.Exit(1)
	}
	dir := os.Args[1]

	// ML-DSA-65 staking key
	dsaPriv, err := mldsa.GenerateKey(rand.Reader, mldsa.MLDSA65)
	if err != nil {
		panic(err)
	}
	dsaPub := dsaPriv.PublicKey.Bytes()
	if err := writePEM(filepath.Join(dir, "mldsa.key"), "ML-DSA-65 PRIVATE KEY", dsaPriv.Bytes()); err != nil {
		panic(err)
	}
	if err := writePEM(filepath.Join(dir, "mldsa.pub"), "ML-DSA-65 PUBLIC KEY", dsaPub); err != nil {
		panic(err)
	}

	// ML-KEM-768 handshake key
	kemPub, kemPriv, err := mlkem.GenerateKeyPair(rand.Reader, mlkem.MLKEM768)
	if err != nil {
		panic(err)
	}
	if err := writePEM(filepath.Join(dir, "mlkem.key"), "ML-KEM-768 PRIVATE KEY", kemPriv.Bytes()); err != nil {
		panic(err)
	}
	if err := writePEM(filepath.Join(dir, "mlkem.pub"), "ML-KEM-768 PUBLIC KEY", kemPub.Bytes()); err != nil {
		panic(err)
	}

	fmt.Printf("ML-DSA-65 priv=%dB pub=%dB; ML-KEM-768 priv=%dB pub=%dB written to %s\n",
		len(dsaPriv.Bytes()), len(dsaPub), len(kemPriv.Bytes()), len(kemPub.Bytes()), dir)
}
