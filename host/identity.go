// SPDX-License-Identifier: BSD-3-Clause-Eco

package host

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/crypto/bls/signer/localsigner"
	"github.com/luxfi/ids"
)

// Identity is who this node is on the network: one staking certificate, which
// names it, and one BLS key, which is how it votes.
//
// The two are not interchangeable and neither substitutes for the other. The
// certificate is what a peer authenticates the TLS session against and what the
// node id is derived from; the BLS key is what a validator set holds and what a
// finality certificate is assembled out of. A node whose certificate is known
// and whose BLS key is not is a peer that cannot vote, which is a real and
// intended state — so they are loaded together and kept apart.
type Identity struct {
	// TLS is the staking certificate and its private key, presented on every
	// dial and every accept.
	TLS tls.Certificate
	// Cert is the parsed leaf, kept because the node id is derived from its
	// exact DER and re-parsing per use would be a second chance to disagree.
	Cert *x509.Certificate
	// NodeID is RIPEMD160(SHA256(cert DER)) — the derivation in luxfi/ids, not a
	// local copy of it.
	NodeID ids.NodeID
	// Signer holds the BLS staking key. Nil when the node has no signing key,
	// which makes it a follower: it reads the network and never votes.
	Signer *localsigner.LocalSigner
}

// staking file names, as a Lux node lays them out on disk.
const (
	certFile   = "staker.crt"
	keyFile    = "staker.key"
	signerFile = "signer.key"
)

// LoadIdentity reads a staking directory.
//
// The certificate and its key are required — without them the node has no name
// and cannot open a session. The BLS key is optional for the reason above.
func LoadIdentity(dir string) (*Identity, error) {
	certPEM, err := os.ReadFile(filepath.Join(dir, certFile))
	if err != nil {
		return nil, fmt.Errorf("staking certificate: %w", err)
	}
	keyPEM, err := os.ReadFile(filepath.Join(dir, keyFile))
	if err != nil {
		return nil, fmt.Errorf("staking key: %w", err)
	}
	id, err := identityFromPEM(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}

	signerBytes, err := os.ReadFile(filepath.Join(dir, signerFile))
	switch {
	case err == nil:
		signer, err := localsigner.FromBytes(signerBytes)
		if err != nil {
			return nil, fmt.Errorf("staking signer key: %w", err)
		}
		id.Signer = signer
	case errors.Is(err, os.ErrNotExist):
		// A node with no BLS key follows; it does not vote.
	default:
		return nil, fmt.Errorf("staking signer key: %w", err)
	}
	return id, nil
}

// EnsureIdentity loads an existing staking identity, or creates one if none exists.
func EnsureIdentity(dir string) (*Identity, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create staking dir: %w", err)
	}
	certPath := filepath.Join(dir, certFile)
	keyPath := filepath.Join(dir, keyFile)
	if _, err := os.Stat(certPath); errors.Is(err, os.ErrNotExist) {
		priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("generate staking key: %w", err)
		}
		template := x509.Certificate{
			SerialNumber: big.NewInt(1),
			Subject: pkix.Name{
				CommonName: "Lux Validator",
			},
			NotBefore: time.Now().Add(-1 * time.Hour),
			NotAfter:  time.Now().Add(10 * 365 * 24 * time.Hour),
			KeyUsage:  x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		}
		derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
		if err != nil {
			return nil, fmt.Errorf("create staking certificate: %w", err)
		}
		certOut, err := os.Create(certPath)
		if err != nil {
			return nil, fmt.Errorf("create cert file: %w", err)
		}
		if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
			certOut.Close()
			return nil, fmt.Errorf("encode cert: %w", err)
		}
		certOut.Close()

		privBytes, err := x509.MarshalECPrivateKey(priv)
		if err != nil {
			return nil, fmt.Errorf("marshal ec key: %w", err)
		}
		keyOut, err := os.Create(keyPath)
		if err != nil {
			return nil, fmt.Errorf("create key file: %w", err)
		}
		if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: privBytes}); err != nil {
			keyOut.Close()
			return nil, fmt.Errorf("encode key: %w", err)
		}
		keyOut.Close()
	}

	signerPath := filepath.Join(dir, signerFile)
	if _, err := os.Stat(signerPath); errors.Is(err, os.ErrNotExist) {
		signer, err := localsigner.New()
		if err != nil {
			return nil, fmt.Errorf("generate bls signer: %w", err)
		}
		if err := os.WriteFile(signerPath, signer.ToBytes(), 0600); err != nil {
			return nil, fmt.Errorf("write signer key: %w", err)
		}
	}

	return LoadIdentity(dir)
}

// identityFromPEM builds an Identity from certificate and key PEM.
func identityFromPEM(certPEM, keyPEM []byte) (*Identity, error) {
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("staking key pair: %w", err)
	}
	if len(pair.Certificate) == 0 {
		return nil, errors.New("staking certificate carries no DER")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("staking certificate: %w", err)
	}
	pair.Leaf = leaf
	return &Identity{
		TLS:    pair,
		Cert:   leaf,
		NodeID: ids.NodeIDFromCert(&ids.Certificate{Raw: leaf.Raw, PublicKey: leaf.PublicKey}),
	}, nil
}

// PublicKey returns the BLS public key this node votes under, or nil when it
// holds no signing key.
func (i *Identity) PublicKey() *bls.PublicKey {
	if i.Signer == nil {
		return nil
	}
	return i.Signer.PublicKey()
}

// Sign produces this node's BLS signature over msg. It is the ONE place the
// staking key is used to sign a vote, so every signature the node makes passes
// through the same call — which is what lets the equivocation journal stand in
// front of all of them.
func (i *Identity) Sign(msg []byte) ([]byte, error) {
	if i.Signer == nil {
		return nil, errors.New("this node holds no staking signing key")
	}
	sig, err := i.Signer.Sign(msg)
	if err != nil {
		return nil, err
	}
	return bls.SignatureToBytes(sig), nil
}
