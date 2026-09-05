// SPDX-License-Identifier: BSD-3-Clause-Eco

package main

// The key material. A differential that cannot sign is a differential that
// cannot move a chain, so the seed is derived here rather than read from a
// file: the light mnemonic is the published dev seed (github.com/luxfi/light),
// and every address a Lux localnet funds comes off it by BIP44. Deriving it
// means the harness holds no secret and needs no fixture.

import (
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/crypto"
	"golang.org/x/crypto/pbkdf2"
)

// Mnemonic is the published Lux dev seed. It is the value of LUX_MNEMONIC on
// every network whose id is >= 1337, and it is public on purpose.
const Mnemonic = "light light light light light light light light light light light energy"

// AnvilKey is the first Anvil/Hardhat account. The C++ node's local genesis
// names it explicitly, so it is the funded account there.
const AnvilKey = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

var secpN, _ = new(big.Int).SetString(
	"fffffffffffffffffffffffffffffffebaaedce6af48a03bbfd25e8cd0364141", 16)

// seed is BIP39: PBKDF2-HMAC-SHA512 over the mnemonic, 2048 rounds, salt
// "mnemonic". No wordlist lookup, because the mnemonic is already the input a
// wallet hashes; a checksum check here would reject nothing this harness feeds.
func seed(mnemonic string) []byte {
	return pbkdf2.Key([]byte(mnemonic), []byte("mnemonic"), 2048, 64, sha512.New)
}

type xkey struct {
	k     *big.Int // private scalar
	chain []byte   // chain code
}

func master(s []byte) xkey {
	h := hmac.New(sha512.New, []byte("Bitcoin seed"))
	h.Write(s)
	sum := h.Sum(nil)
	return xkey{k: new(big.Int).SetBytes(sum[:32]), chain: sum[32:]}
}

// derive walks one BIP32 level. Only the hardened and non-hardened private
// forms are needed; a public derivation would be a second code path with no
// caller.
func (x xkey) derive(index uint32) xkey {
	h := hmac.New(sha512.New, x.chain)
	if index >= 0x80000000 {
		h.Write(append([]byte{0}, leftPad(x.k.Bytes(), 32)...))
	} else {
		priv := &ecdsa.PrivateKey{}
		priv.D = x.k
		priv.PublicKey.Curve = crypto.S256()
		priv.PublicKey.X, priv.PublicKey.Y = crypto.S256().ScalarBaseMult(leftPad(x.k.Bytes(), 32))
		h.Write(crypto.CompressPubkey(&priv.PublicKey))
	}
	var idx [4]byte
	binary.BigEndian.PutUint32(idx[:], index)
	h.Write(idx[:])
	sum := h.Sum(nil)

	k := new(big.Int).SetBytes(sum[:32])
	k.Add(k, x.k)
	k.Mod(k, secpN)
	return xkey{k: k, chain: sum[32:]}
}

func leftPad(b []byte, n int) []byte {
	if len(b) >= n {
		return b
	}
	out := make([]byte, n)
	copy(out[n-len(b):], b)
	return out
}

// Account is a key and the address it recovers to.
type Account struct {
	Priv *ecdsa.PrivateKey
	Addr common.Address
	Path string
}

func fromScalar(k *big.Int, path string) (Account, error) {
	priv, err := crypto.ToECDSA(leftPad(k.Bytes(), 32))
	if err != nil {
		return Account{}, err
	}
	return Account{Priv: priv, Addr: common.Address(crypto.PubkeyToAddress(priv.PublicKey)), Path: path}, nil
}

// FromHex builds an account from a raw secp256k1 scalar in hex.
func FromHex(h, path string) (Account, error) {
	priv, err := crypto.HexToECDSA(h)
	if err != nil {
		return Account{}, err
	}
	return Account{Priv: priv, Addr: common.Address(crypto.PubkeyToAddress(priv.PublicKey)), Path: path}, nil
}

// BIP44 derives m/44'/coin'/0'/0/index off the given mnemonic. Lux funds both
// coin 60 (the Ethereum path every browser wallet uses) and coin 9000 (the path
// the Lux wallet uses), and which one holds a chain's genesis money is a fact
// about that genesis, not something the harness may assume.
func BIP44(mnemonic string, coin, index uint32) (Account, error) {
	const h = uint32(0x80000000)
	x := master(seed(mnemonic))
	for _, step := range []uint32{44 + h, coin + h, 0 + h, 0, index} {
		x = x.derive(step)
	}
	return fromScalar(x.k, fmt.Sprintf("m/44'/%d'/0'/0/%d", coin, index))
}

// Candidates is every account the harness is willing to try to spend from: the
// Anvil key the C++ genesis names, and the first stretch of both BIP44 paths
// off the light mnemonic.
func Candidates(depth uint32) []Account {
	var out []Account
	if a, err := FromHex(AnvilKey, "anvil/0"); err == nil {
		out = append(out, a)
	}
	for _, coin := range []uint32{60, 9000} {
		for i := uint32(0); i < depth; i++ {
			if a, err := BIP44(Mnemonic, coin, i); err == nil {
				out = append(out, a)
			}
		}
	}
	return out
}
