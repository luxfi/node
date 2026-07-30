// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"testing"

	"github.com/luxfi/crypto/bls"
)

// A validator's registered BLS key must verify in EITHER G1 encoding.
//
// Hanzo L1 36963 halted on exactly this: every validator in its set carries a
// 96-byte UNCOMPRESSED G1 key, the verifier parsed only 48-byte COMPRESSED, so
// key parsing failed for every voter and no vote could ever be counted. The
// chain sat at one height forever while every other signal looked healthy.
func TestValidatorPublicKeyParsesInBothG1Encodings(t *testing.T) {
	sk, err := bls.NewSecretKey()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	pub := sk.PublicKey()

	compressed := bls.PublicKeyToCompressedBytes(pub)
	uncompressed := bls.PublicKeyToUncompressedBytes(pub)

	if len(compressed) != bls.PublicKeyLen {
		t.Fatalf("compressed len = %d, want %d", len(compressed), bls.PublicKeyLen)
	}
	if len(uncompressed) != 2*bls.PublicKeyLen {
		t.Fatalf("uncompressed len = %d, want %d", len(uncompressed), 2*bls.PublicKeyLen)
	}

	// The real test: BOTH encodings must parse, and both must verify the SAME
	// signature. Parsing alone is not enough — a mis-parsed key would still fail
	// bls.Verify and strand the chain the same way.
	msg := []byte("canonical vote message")
	sig, err := sk.Sign(msg)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	for name, raw := range map[string][]byte{
		"compressed(48)":   compressed,
		"uncompressed(96)": uncompressed,
	} {
		pk, err := blsPublicKeyFromValidatorBytes(raw)
		if err != nil || pk == nil {
			t.Errorf("%s: parse failed: %v", name, err)
			continue
		}
		if !bls.Verify(pk, sig, msg) {
			t.Errorf("%s: parsed but did not verify a valid signature", name)
		}
	}
}

// Widening what can be PARSED must not widen what is ACCEPTED.
func TestBadValidatorPublicKeyIsStillRejected(t *testing.T) {
	for name, raw := range map[string][]byte{
		"empty":     {},
		"short":     make([]byte, 47),
		"between":   make([]byte, 64),
		"too long":  make([]byte, 128),
		"all zeros": make([]byte, bls.PublicKeyLen),
	} {
		pk, err := blsPublicKeyFromValidatorBytes(raw)
		if err == nil && pk != nil {
			// A zero-length-correct blob may parse; it must NOT verify anything.
			if sig, sErr := bls.SignatureFromBytes(make([]byte, bls.SignatureLen)); sErr == nil && sig != nil {
				if bls.Verify(pk, sig, []byte("m")) {
					t.Errorf("%s: garbage key verified a signature", name)
				}
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
