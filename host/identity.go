// SPDX-License-Identifier: BSD-3-Clause-Eco

package host

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
