// SPDX-License-Identifier: BSD-3-Clause-Eco

// The F-chain half of the corpus, and the Go reference's answers for it.
//
// BUILDING. F exports its transaction whole: the struct, `Transaction.Bytes`
// and `ParseTransaction`, which is canonical-or-nothing. Every vector's bytes
// come out of the chain's own encoder, and the four hashes a transaction has
// to agree with — the ciphertext handle, the permit id, the committee digest,
// and the payer's address — are recomputed here from the definitions the chain
// documents and then CHECKED against it: a vector whose subject the reference
// does not accept stops the emit.
//
// The payer's signatures are real ML-DSA-65, made under the FIPS 204
// deterministic variant so the corpus is the same bytes on every run. A hedged
// signature would move every vector that carries one on every run, and a
// corpus that moves cannot tell drift from noise.
//
// ANSWERING. `evalF` runs the real VM against a FUNDED chain: the corpus's
// genesis credits the payer and seats a committee, so a transaction that is
// correct reaches OK and a transaction that is wrong is refused for the rule it
// broke rather than for an empty balance. Each vector gets its own VM, because
// admission REMEMBERS — a nonce it took, an effect it claimed — and a corpus
// whose answers depended on the order it was read in would not be a corpus.
//
// The two layers are the ones F itself names:
//
//	syntactic — `SyntacticVerify`: well-formed and priceable, without state
//	exec      — `SubmitTx`: the signature, the nonce, the effect, the
//	            authorisation, and whether the payer can pay
package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/luxfi/chains/fhevm"
	"github.com/luxfi/chains/mpcvm/fhe"
	"github.com/luxfi/crypto/mldsa"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/log"
	"github.com/luxfi/runtime"
	luxvm "github.com/luxfi/vm"
)

// The chain every F vector is signed for. A signature is bound to it, so a
// transaction built for one F-chain does not authenticate on another.
const (
	fChain     = 50
	fScheme    = "ckks-n14"
	fGasLimit  = 5_000_000
	fAllocated = 1 << 40
)

// stream is a deterministic byte source. Key generation and the payer's
// address derive from it, so the corpus's payer is the same account on every
// run of the generator.
type stream struct{ n byte }

func (s *stream) Read(p []byte) (int, error) {
	for i := range p {
		s.n++
		p[i] = s.n
	}
	return len(p), nil
}

// fKey is the corpus's one payer, and also the one committee member. A
// committee whose seat is the payer's own key is what lets a fulfil vector be
// signed by somebody the chain will accept as a member.
func fKey() *mldsa.PrivateKey {
	k, err := mldsa.GenerateKey(&stream{}, mldsa.MLDSA65)
	must(err)
	return k
}

var fPayerKey = fKey()

func fPubKey() []byte { return fPayerKey.PublicKey.Bytes() }

// fAccount is the chain's own derivation of an address from a public key:
// sha256 of the key, truncated to a short id. It is checked at emit — a payer
// the chain derives differently fails `authenticate` with a payer mismatch,
// and the assertion below refuses to write that corpus.
func fAccount(pub []byte) [20]byte {
	h := sha256.Sum256(pub)
	var a [20]byte
	copy(a[:], h[:20])
	return a
}

func lenPrefixed(h interface{ Write([]byte) (int, error) }, b []byte) {
	var u8 [8]byte
	binary.BigEndian.PutUint64(u8[:], uint64(len(b)))
	_, _ = h.Write(u8[:])
	_, _ = h.Write(b)
}

// fHandle names a ciphertext by its content: the digest of the off-chain body
// under a scheme. The chain refuses a registration whose subject disagrees, so
// this derivation is one the reference confirms rather than one it is told.
func fHandle(digest [32]byte, scheme string) [32]byte {
	h := sha256.New()
	h.Write([]byte("fhevm/ct/"))
	h.Write(digest[:])
	lenPrefixed(h, []byte(scheme))
	return [32]byte(h.Sum(nil))
}

// fCommitteeDigest names an epoch proposal by everything in it. Same contract:
// the chain refuses a proposal whose subject disagrees.
func fCommitteeDigest(epoch uint64, threshold int, publicKey []byte, c []fhe.CommitteeMember) [32]byte {
	h := sha256.New()
	h.Write([]byte("fhevm/epoch/"))
	var u8 [8]byte
	binary.BigEndian.PutUint64(u8[:], epoch)
	h.Write(u8[:])
	binary.BigEndian.PutUint64(u8[:], uint64(threshold))
	h.Write(u8[:])
	lenPrefixed(h, publicKey)
	binary.BigEndian.PutUint64(u8[:], uint64(len(c)))
	h.Write(u8[:])
	for _, m := range c {
		id := m.NodeID
		h.Write(id[:])
		lenPrefixed(h, m.PublicKey)
		binary.BigEndian.PutUint64(u8[:], m.Weight)
		h.Write(u8[:])
		binary.BigEndian.PutUint64(u8[:], uint64(m.Index))
		h.Write(u8[:])
	}
	return [32]byte(h.Sum(nil))
}

func fCommittee() []fhe.CommitteeMember {
	return []fhe.CommitteeMember{{
		NodeID:    nodeID(7),
		PublicKey: fPubKey(),
		Weight:    1,
		Index:     0,
	}}
}

// fNetworkKey is the committee's joint public key. Nothing here verifies
// against it — it is a recorded field the chain refuses to be empty — so it is
// a fixed literal rather than a key nobody holds the other half of.
func fNetworkKey() []byte { return zBytes(0x77, 32) }

func fGenesis() []byte {
	acct := fAccount(fPubKey())
	g := map[string]any{
		"version":   1,
		"timestamp": genesisTime,
		"alloc":     map[string]uint64{hex.EncodeToString(acct[:]): fAllocated},
		"committee": fCommittee(),
		"threshold": 1,
		"publicKey": fNetworkKey(),
	}
	b, err := json.Marshal(g)
	must(err)
	return b
}

func fPayload(v any) []byte {
	b, err := json.Marshal(v)
	must(err)
	return b
}

// fSign completes a transaction: the payer's key, its address, and a
// deterministic ML-DSA-65 signature over the bytes the chain will re-derive.
func fSign(tx *fhevm.Transaction) *fhevm.Transaction {
	tx.Payer = fAccount(fPubKey())
	tx.Auth = fPubKey()
	sig, err := fPayerKey.SignCtxDeterministic(tx.SigningBytes(id(fChain)), nil)
	must(err)
	tx.Sig = sig
	return tx
}

func fRegister(nonce uint64, digest byte) *fhevm.Transaction {
	d := [32]byte(zBytes(digest, 32))
	tx := &fhevm.Transaction{
		Type:     fhevm.TxRegisterCiphertext,
		Scheme:   fScheme,
		Subject:  fHandle(d, fScheme),
		GasLimit: fGasLimit,
		Nonce:    nonce,
		Payload: fPayload(fhevm.RegisterPayload{
			Digest: d, Type: 1, Level: 3, Size: 4096,
		}),
	}
	return fSign(tx)
}

func fGrant(nonce uint64, handle [32]byte, ops uint32) *fhevm.Transaction {
	return fSign(&fhevm.Transaction{
		Type:     fhevm.TxGrantPermit,
		Subject:  handle,
		GasLimit: fGasLimit,
		Nonce:    nonce,
		Payload: fPayload(fhevm.GrantPayload{
			Grantee: fAccount(fPubKey()), Operations: ops, Expiry: 0,
		}),
	})
}

func fRevoke(nonce uint64, permit [32]byte) *fhevm.Transaction {
	return fSign(&fhevm.Transaction{
		Type:     fhevm.TxRevokePermit,
		Subject:  permit,
		GasLimit: fGasLimit,
		Nonce:    nonce,
		Payload:  fPayload(fhevm.RevokePayload{Reason: "conformance"}),
	})
}

func fRequest(nonce uint64, handle, permit [32]byte) *fhevm.Transaction {
	return fSign(&fhevm.Transaction{
		Type: fhevm.TxRequestDecrypt,
		// A decryption is priced per scheme, so the request names one. The
		// operations that touch no ciphertext — revoke, fulfil, advance —
		// carry none, and pricing them under a scheme would be a price for
		// work nobody does.
		Scheme:   fScheme,
		Subject:  handle,
		GasLimit: fGasLimit,
		Nonce:    nonce,
		Payload: fPayload(fhevm.RequestPayload{
			PermitID: permit,
			Callback: [20]byte(zBytes(0x33, 20)),
			Selector: [4]byte{1, 2, 3, 4},
			Expiry:   0,
		}),
	})
}

func fFulfill(nonce uint64, request [32]byte) *fhevm.Transaction {
	return fSign(&fhevm.Transaction{
		Type:     fhevm.TxFulfillDecrypt,
		Subject:  request,
		GasLimit: fGasLimit,
		Nonce:    nonce,
		Payload:  fPayload(fhevm.FulfillPayload{Result: [32]byte(zBytes(0x44, 32))}),
	})
}

func fAdvance(nonce uint64, epoch uint64) *fhevm.Transaction {
	c := fCommittee()
	key := fNetworkKey()
	return fSign(&fhevm.Transaction{
		Type:     fhevm.TxAdvanceEpoch,
		Subject:  fCommitteeDigest(epoch, 1, key, c),
		GasLimit: fGasLimit,
		Nonce:    nonce,
		Payload: fPayload(fhevm.AdvancePayload{
			Epoch: epoch, Committee: c, Threshold: 1, PublicKey: key,
		}),
	})
}

func fVectors() []Vector {
	handle := fHandle([32]byte(zBytes(0x21, 32)), fScheme)
	permit := [32]byte(zBytes(0x22, 32))

	// The genesis this chain is born from, carried as a vector.
	//
	// It is an INPUT every implementation needs and no transaction contains:
	// the funded payer, the committee, the threshold and the network key all
	// come from it, and a chain given a different one refuses transactions for
	// reasons that have nothing to do with their bytes. Carrying it means no
	// evaluator has to reconstruct it — and the answer, the id the chain's own
	// genesis block takes, says whether two implementations applied it the
	// same way.
	v := []Vector{
		vec("F_GENESIS", "F", "genesis", fGenesis()),

		// The six operations a confidential value's life is made of.
		vec("F_REGISTER", "F", "tx", fRegister(1, 0x21).Bytes()),
		vec("F_GRANT", "F", "tx", fGrant(1, handle, fhe.PermitOpDecrypt).Bytes()),
		vec("F_REVOKE", "F", "tx", fRevoke(1, permit).Bytes()),
		vec("F_REQUEST", "F", "tx", fRequest(1, handle, permit).Bytes()),
		vec("F_FULFILL", "F", "tx", fFulfill(1, permit).Bytes()),
		vec("F_ADVANCE", "F", "tx", fAdvance(1, 1).Bytes()),
	}

	// Well-formedness. Each is a correct transaction with exactly one thing
	// wrong, so an implementation that misses the check answers OK where the
	// others name it.
	badType := fRegister(1, 0x21)
	badType.Type = 9
	zeroNonce := fRegister(0, 0x21)
	wrongSubject := fRegister(1, 0x21)
	wrongSubject.Subject = [32]byte(zBytes(0x99, 32))

	emptyDigest := fSign(&fhevm.Transaction{
		Type: fhevm.TxRegisterCiphertext, Scheme: fScheme, GasLimit: fGasLimit, Nonce: 1,
		Subject: fHandle([32]byte{}, fScheme),
		Payload: fPayload(fhevm.RegisterPayload{Type: 1, Level: 3, Size: 4096}),
	})
	zeroSize := fSign(&fhevm.Transaction{
		Type: fhevm.TxRegisterCiphertext, Scheme: fScheme, GasLimit: fGasLimit, Nonce: 1,
		Subject: fHandle([32]byte(zBytes(0x21, 32)), fScheme),
		Payload: fPayload(fhevm.RegisterPayload{Digest: [32]byte(zBytes(0x21, 32)), Type: 1, Level: 3}),
	})
	negativeLevel := fSign(&fhevm.Transaction{
		Type: fhevm.TxRegisterCiphertext, Scheme: fScheme, GasLimit: fGasLimit, Nonce: 1,
		Subject: fHandle([32]byte(zBytes(0x21, 32)), fScheme),
		Payload: fPayload(fhevm.RegisterPayload{Digest: [32]byte(zBytes(0x21, 32)), Type: 1, Level: -1, Size: 4096}),
	})
	unknownScheme := fRegister(1, 0x21)
	unknownScheme.Scheme = "no-such-scheme"
	unknownScheme = fSign(unknownScheme)

	noOps := fGrant(1, handle, 0)
	unknownOps := fGrant(1, handle, 1<<20)
	noPermit := fRequest(1, handle, [32]byte{})
	noResult := fSign(&fhevm.Transaction{
		Type: fhevm.TxFulfillDecrypt, Subject: permit, GasLimit: fGasLimit, Nonce: 1,
		Payload: fPayload(fhevm.FulfillPayload{}),
	})
	emptyCommittee := fSign(&fhevm.Transaction{
		Type: fhevm.TxAdvanceEpoch, Subject: [32]byte{}, GasLimit: fGasLimit, Nonce: 1,
		Payload: fPayload(fhevm.AdvancePayload{Epoch: 1, Threshold: 1, PublicKey: fNetworkKey()}),
	})
	// A member the schema does not describe. F decodes payloads with unknown
	// fields refused, because a chain that keeps what it is given and ignores
	// what it does not recognise is a byte channel.
	unknownField := fRegister(1, 0x21)
	unknownField.Payload = []byte(`{"digest":"` +
		hex.EncodeToString(zBytes(0x21, 32)) + `","type":1,"level":3,"size":4096,"body":"AAAA"}`)
	unknownField = fSign(unknownField)

	// Authorisation. The signature is real, so what is wrong with each of
	// these is exactly the thing its name says.
	unsigned := fRegister(1, 0x21)
	unsigned.Auth = nil
	unsigned.Sig = nil
	tampered := fRegister(1, 0x21)
	tampered.Sig = append([]byte(nil), tampered.Sig...)
	tampered.Sig[0] ^= 0xFF
	otherPayer := fRegister(1, 0x21)
	otherPayer.Payer = [20]byte(zBytes(0x66, 20))
	shortAuth := fRegister(1, 0x21)
	shortAuth.Auth = shortAuth.Auth[:8]
	shortSig := fRegister(1, 0x21)
	shortSig.Sig = shortSig.Sig[:8]

	// A transaction signed for another F-chain. Its payer holds the same
	// address on both, so nothing but the chain binding stops it spending here.
	foreign := &fhevm.Transaction{
		Type: fhevm.TxRegisterCiphertext, Scheme: fScheme, GasLimit: fGasLimit, Nonce: 1,
		Subject: fHandle([32]byte(zBytes(0x21, 32)), fScheme),
		Payload: fPayload(fhevm.RegisterPayload{Digest: [32]byte(zBytes(0x21, 32)), Type: 1, Level: 3, Size: 4096}),
		Payer:   fAccount(fPubKey()),
		Auth:    fPubKey(),
	}
	fsig, err := fPayerKey.SignCtxDeterministic(foreign.SigningBytes(id(fChain+1)), nil)
	must(err)
	foreign.Sig = fsig

	// A nonce the payer has not reached. Replay and gap are the two sides of
	// one rule, and a chain that enforced only "greater than the last" would
	// take the gap and then never build.
	gap := fRegister(9, 0x21)

	// A registration whose CIPHERTEXT was swapped after the payer signed it.
	// The handle in Subject still names the ciphertext that was signed for;
	// the payload now describes a different one.
	//
	// This is the attack the F-chain's signing preimage exists to stop, and it
	// is not the same as a corrupted signature. Subject is bound into the
	// content the payer signs, and syntactic_verify requires it to equal what
	// the payload derives, so the swap has to break one of the two: either the
	// handle no longer matches the ciphertext, or the signature no longer
	// matches the content. It must never be accepted with the substituted
	// ciphertext under the original owner's authority.
	swapped := fRegister(1, 0x21)
	swapped.Payload = fPayload(fhevm.RegisterPayload{
		Digest: [32]byte(zBytes(0x99, 32)), Type: 1, Level: 3, Size: 4096,
	})

	v = append(v,
		vec("F_TX_UNKNOWN_TYPE", "F", "tx", badType.Bytes()),
		vec("F_TX_ZERO_NONCE", "F", "tx", zeroNonce.Bytes()),
		vec("F_TX_SUBJECT_MISMATCH", "F", "tx", wrongSubject.Bytes()),
		vec("F_TX_EMPTY_DIGEST", "F", "tx", emptyDigest.Bytes()),
		vec("F_TX_ZERO_SIZE", "F", "tx", zeroSize.Bytes()),
		vec("F_TX_NEGATIVE_LEVEL", "F", "tx", negativeLevel.Bytes()),
		vec("F_TX_UNKNOWN_SCHEME", "F", "tx", unknownScheme.Bytes()),
		vec("F_TX_GRANT_NO_OPS", "F", "tx", noOps.Bytes()),
		vec("F_TX_GRANT_UNKNOWN_OPS", "F", "tx", unknownOps.Bytes()),
		vec("F_TX_REQUEST_NO_PERMIT", "F", "tx", noPermit.Bytes()),
		vec("F_TX_FULFILL_NO_RESULT", "F", "tx", noResult.Bytes()),
		vec("F_TX_EMPTY_COMMITTEE", "F", "tx", emptyCommittee.Bytes()),
		vec("F_TX_UNKNOWN_PAYLOAD_FIELD", "F", "tx", unknownField.Bytes()),

		vec("F_TX_TAMPERED_CIPHERTEXT", "F", "tx", swapped.Bytes()),

		vec("F_TX_UNSIGNED", "F", "tx", unsigned.Bytes()),
		vec("F_TX_TAMPERED_SIGNATURE", "F", "tx", tampered.Bytes()),
		vec("F_TX_PAYER_MISMATCH", "F", "tx", otherPayer.Bytes()),
		vec("F_TX_SHORT_AUTH", "F", "tx", shortAuth.Bytes()),
		vec("F_TX_SHORT_SIGNATURE", "F", "tx", shortSig.Bytes()),
		vec("F_TX_FOREIGN_CHAIN", "F", "tx", foreign.Bytes()),
		vec("F_TX_NONCE_GAP", "F", "tx", gap.Bytes()),
	)

	// The wire. F's parser is canonical-or-nothing: it re-serializes what it
	// decoded and refuses anything that does not come back byte-identical, so
	// exactly one byte string authenticates per transaction.
	good := fRegister(1, 0x21).Bytes()
	trailing := append(append([]byte{}, good...), 0xFF, 0xFF)
	v = append(v,
		vec("F_WIRE_TRAILING_BYTES", "F", "tx", trailing),
		vec("F_WIRE_TRUNCATED", "F", "tx", good[:len(good)/2]),
		vec("F_WIRE_EMPTY", "F", "tx", nil),
		vec("F_WIRE_ONE_BYTE", "F", "tx", []byte{0x5a}),
	)

	v = append(v, fDecoderVectors(handle)...)

	fAssert(v)
	return v
}

// The decoder's acceptance set, which on this chain is a consensus question.
//
// A payload is an opaque byte string the payer chooses, and whether it DECODES
// decides whether the transaction is valid — so every rule Go's encoding/json
// happens to have is a rule the F-chain has. These are the rules that are easy
// to get subtly wrong in another language, one vector each, and each of them is
// a correct transaction with one thing done to its payload. Every answer below
// is the Go chain's, whatever it is: the point is that all three implementations
// give the SAME one.
func fDecoderVectors(handle [32]byte) []Vector {
	// raw builds a signed transaction around payload bytes written by hand,
	// which is the only way to pose these questions — json.Marshal cannot emit
	// a duplicate member, a folded name, or a stray brace.
	raw := func(t uint8, scheme string, subject [32]byte, payload string) *fhevm.Transaction {
		return fSign(&fhevm.Transaction{
			Type: t, Scheme: scheme, Subject: subject,
			GasLimit: fGasLimit, Nonce: 1, Payload: []byte(payload),
		})
	}
	// advance builds a COMPLETE, correct epoch proposal, marshals it, and hands
	// the bytes to `edit` — which is how a vector varies one spelling of one
	// member and nothing else. The subject is the digest of the proposal, so an
	// edit that survives decoding reaches a transaction the chain admits.
	advance := func(edit func(string) string) *fhevm.Transaction {
		c := fCommittee()
		key := fNetworkKey()
		body := string(fPayload(fhevm.AdvancePayload{
			Epoch: 1, Committee: c, Threshold: 1, PublicKey: key,
		}))
		return fSign(&fhevm.Transaction{
			Type:     fhevm.TxAdvanceEpoch,
			Subject:  fCommitteeDigest(1, 1, key, c),
			GasLimit: fGasLimit,
			Nonce:    1,
			Payload:  []byte(edit(body)),
		})
	}
	digest := [32]byte(zBytes(0x21, 32))
	register := func(payload string) *fhevm.Transaction {
		return raw(fhevm.TxRegisterCiphertext, fScheme, fHandle(digest, fScheme), payload)
	}
	// The register payload, spelled out, so each vector below can vary one
	// piece of it and nothing else.
	digestArray := "["
	for i, b := range digest {
		if i > 0 {
			digestArray += ","
		}
		digestArray += fmt.Sprintf("%d", b)
	}
	digestArray += "]"
	good := `{"digest":` + digestArray + `,"type":1,"level":3,"size":4096}`

	// A literal null where a struct belongs. Go decodes it as the ZERO struct
	// and does not error, so what refuses the transaction is whatever rule the
	// zero payload then breaks — which is a different answer per operation, and
	// that is the point: an implementation that refused null outright would
	// name the wrong rule on every one of them.
	nullRevoke := raw(fhevm.TxRevokePermit, "", [32]byte(zBytes(0x22, 32)), `null`)
	nullAdvance := raw(fhevm.TxAdvanceEpoch, "", [32]byte{}, `null`)
	nullRegister := register(`null`)

	// Trailing content, which is not one rule but two. Go reads a payload
	// through a Decoder and asks dec.More(), and More answers "is there another
	// ELEMENT" — so a stray ']' or '}' is NOT trailing content, while a comma or
	// a second document is.
	braceAfter := register(good + `}`)
	bracketAfter := register(good + `]`)
	commaAfter := register(good + `,`)
	documentAfter := register(good + `{}`)

	// Member names. Go matches exactly, then by unicode.SimpleFold — which is
	// not ASCII case folding: U+017F folds onto s and U+212A onto k, so
	// "ſize" names Size and "publicKey" names PublicKey.
	upperKeys := register(`{"DIGEST":` + digestArray + `,"TYPE":1,"Level":3,"SIZE":4096}`)
	longS := register(`{"digest":` + digestArray + `,"type":1,"level":3,"` + "\u017F" + `ize":4096}`)

	// The Kelvin sign folds onto k, so it names PublicKey — and the vector is
	// built on a COMPLETE epoch proposal so that the answer turns on whether
	// the key was read: if it was, the proposal is whole and the transaction is
	// admitted; if it was not, the network key is missing and the committee is
	// refused. A proposal with a null committee would have been refused either
	// way and told nobody anything.
	kelvin := advance(func(body string) string {
		return strings.Replace(body, `"publicKey":`, `"public`+"\u212A"+`ey":`, 1)
	})

	// The same shape for a base64 word broken across a line, which
	// encoding/base64 ignores: the bytes are identical, so a reader that
	// ignores the newline reaches a whole proposal and one that does not
	// reaches a proposal with no key.
	wrappedKey := advance(func(body string) string {
		enc := base64.StdEncoding.EncodeToString(fNetworkKey())
		return strings.Replace(body, `"`+enc+`"`,
			`"`+enc[:8]+`\r\n`+enc[8:]+`"`, 1)
	})

	// Two members naming ONE field. Go resolves each key on its own and then
	// writes it, so the LATER key wins whichever way each of them matched — and
	// an implementation that preferred the exact match would read a different
	// size out of the same bytes.
	dupLastWins := register(`{"digest":` + digestArray + `,"type":1,"level":3,"size":4096,"SIZE":8192}`)

	// A Go array discards elements past its length WITHOUT asking what type
	// they are, and leaves the ones it never reached at zero.
	longArray := register(`{"digest":` + digestArray[:len(digestArray)-1] + `,"x",999],"type":1,"level":3,"size":4096}`)
	shortArray := register(`{"digest":[1,2,3],"type":1,"level":3,"size":4096}`)

	// An id word that is not cb58. An address takes "" and a quoted "null" as
	// the zero address before it reaches cb58 at all; a node id does the same;
	// the bare "NodeID-" prefix does not.
	emptyGrantee := raw(fhevm.TxGrantPermit, "", handle, `{"grantee":"","operations":1,"expiry":0}`)
	wordedNullGrantee := raw(fhevm.TxGrantPermit, "", handle, `{"grantee":"null","operations":1,"expiry":0}`)

	// NOT here: the JSON nesting cap. Go allows 10,000 open containers and
	// refuses the 10,001st, and both sides of that boundary are refused for the
	// SAME compared verdict — the runner weighs parse/kind/id/syntactic/exec
	// and not the sentence — so a vector could not tell a wrong cap from a
	// right one. It would cost 1.3 MB of brackets to say nothing. The cap is
	// pinned where it can actually be seen, in the C++ suite's json test,
	// against the two answers Go was asked for directly.

	return []Vector{
		vec("F_JSON_NULL_REVOKE", "F", "tx", nullRevoke.Bytes()),
		vec("F_JSON_NULL_ADVANCE", "F", "tx", nullAdvance.Bytes()),
		vec("F_JSON_NULL_REGISTER", "F", "tx", nullRegister.Bytes()),
		vec("F_JSON_TRAILING_BRACE", "F", "tx", braceAfter.Bytes()),
		vec("F_JSON_TRAILING_BRACKET", "F", "tx", bracketAfter.Bytes()),
		vec("F_JSON_TRAILING_COMMA", "F", "tx", commaAfter.Bytes()),
		vec("F_JSON_TRAILING_DOCUMENT", "F", "tx", documentAfter.Bytes()),
		vec("F_JSON_UPPERCASE_KEYS", "F", "tx", upperKeys.Bytes()),
		vec("F_JSON_LONG_S_KEY", "F", "tx", longS.Bytes()),
		vec("F_JSON_KELVIN_KEY", "F", "tx", kelvin.Bytes()),
		vec("F_JSON_DUPLICATE_KEY", "F", "tx", dupLastWins.Bytes()),
		vec("F_JSON_ARRAY_TAIL_DISCARDED", "F", "tx", longArray.Bytes()),
		vec("F_JSON_ARRAY_SHORT", "F", "tx", shortArray.Bytes()),
		vec("F_JSON_BASE64_NEWLINE", "F", "tx", wrappedKey.Bytes()),
		vec("F_JSON_EMPTY_GRANTEE", "F", "tx", emptyGrantee.Bytes()),
		vec("F_JSON_WORDED_NULL_GRANTEE", "F", "tx", wordedNullGrantee.Bytes()),
	}
}

// fAssert holds the emit to the claims this half of the corpus rests on: the
// four derivations above are the chain's, and the chain is funded.
func fAssert(v []Vector) {
	by := map[string]Result{}
	for _, vec := range v {
		by[vec.ID] = evalF(vec)
	}

	// The subject of a register and of an epoch proposal is a hash this file
	// recomputes. The chain refuses one that disagrees, so these two passing
	// SyntacticVerify is the reference confirming both derivations.
	for _, want := range []string{"F_REGISTER", "F_ADVANCE"} {
		if by[want].Syntactic != VOK {
			panic(fmt.Sprintf("%s: the subject this file derives is not the chain's: %s",
				want, by[want].Note))
		}
	}

	// The payer's address is a third derivation, and the signature a fourth.
	// A register that reaches OK is both of them confirmed, and the funded
	// genesis with them — the payer could not pay otherwise.
	if by["F_REGISTER"].Exec != VOK {
		panic("F_REGISTER does not settle on the funded chain: " + by["F_REGISTER"].Note)
	}
	if by["F_TX_PAYER_MISMATCH"].Exec == VOK {
		panic("F_TX_PAYER_MISMATCH was expected to name an address the key does not derive")
	}
}

// fvm returns a fresh F-chain, funded and with a committee seated.
//
// Fresh per vector, and deliberately: admission remembers the nonce it took and
// the effect it claimed, so a shared chain would answer a vector differently
// depending on which vectors were read before it.
// fvm stands a chain up on the corpus's own genesis, which is the one the F
// vectors are judged on. fvmFrom takes the bytes so the genesis vector and the
// transaction vectors cannot drift onto two different chains.
func fvm() (*fhevm.VM, error) { return fvmFrom(fGenesis()) }

func fvmFrom(genesis []byte) (*fhevm.VM, error) {
	vm := &fhevm.VM{}
	err := vm.Initialize(context.Background(), luxvm.Init{
		Runtime: &runtime.Runtime{
			NodeID:    nodeID(1),
			NetworkID: networkID,
			ChainID:   id(fChain),
			Log:       log.Noop(),
		},
		DB:      memdb.New(),
		Log:     log.Noop(),
		Genesis: genesis,
	})
	if err != nil {
		return nil, err
	}
	return vm, nil
}

func evalF(v Vector) Result {
	r := Result{ID: v.ID, Kind: none, Hash: none, Syntactic: none, Exec: none}
	b, ok := wireOf(v)
	if !ok {
		r.Parse = VInternal
		r.Note = "corpus wire is not hex"
		return r
	}
	if v.Op == "genesis" {
		return evalFGenesis(r, b)
	}

	tx, err := fhevm.ParseTransaction(b)
	if err != nil {
		r.Parse = VMalformed
		r.Syntactic = VMalformed
		r.Exec = VMalformed
		r.Note = trim(err.Error())
		return r
	}
	r.Parse = "ok"
	r.Kind = fKindName(tx.Type)
	txID := tx.ID()
	r.Hash = hex.EncodeToString(txID[:])

	if err := tx.SyntacticVerify(); err != nil {
		class := classify(err)
		r.Syntactic = class
		r.Exec = class
		r.Note = trim(err.Error())
		return r
	}
	r.Syntactic = VOK

	vm, err := fvm()
	if err != nil {
		r.Exec = VInternal
		r.Note = "the reference VM did not start: " + err.Error()
		return r
	}
	if _, err := vm.SubmitTx(tx); err != nil {
		r.Exec = classify(err)
		r.Note = trim(err.Error())
		return r
	}
	r.Exec = VOK
	r.Note = "admitted by the funded chain"
	return r
}

// fKindName names the operation the way the wire does, with no language's
// spelling in it.
func fKindName(t uint8) string {
	switch t {
	case fhevm.TxRegisterCiphertext:
		return "RegisterCiphertext"
	case fhevm.TxGrantPermit:
		return "GrantPermit"
	case fhevm.TxRevokePermit:
		return "RevokePermit"
	case fhevm.TxRequestDecrypt:
		return "RequestDecrypt"
	case fhevm.TxFulfillDecrypt:
		return "FulfillDecrypt"
	case fhevm.TxAdvanceEpoch:
		return "AdvanceEpoch"
	default:
		return "unknown"
	}
}

// evalFGenesis stands the chain up on the corpus's own genesis bytes and
// answers with the id its genesis block took.
//
// The genesis is not a transaction, so nothing about it is syntactic; what is
// compared is whether two implementations handed the same configuration reach
// the same first block. They can fail to: the F-chain's block id is hashed
// from the chain id, the parent, the height, the timestamp and the
// transactions, and every one of those but the chain id comes from this file.
func evalFGenesis(r Result, genesis []byte) Result {
	r.Parse = "ok"
	r.Kind = "Genesis"
	vm, err := fvmFrom(genesis)
	if err != nil {
		r.Syntactic = VInternal
		r.Exec = VInternal
		r.Note = "the reference VM did not start: " + trim(err.Error())
		return r
	}
	last, err := vm.LastAccepted(context.Background())
	if err != nil {
		r.Syntactic = VInternal
		r.Exec = VInternal
		r.Note = "the chain has no last-accepted block: " + trim(err.Error())
		return r
	}
	r.Hash = hex.EncodeToString(last[:])
	r.Syntactic = VOK
	r.Exec = VOK
	r.Note = "the chain the F vectors are judged on"
	return r
}
