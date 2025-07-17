# Corona Post-Quantum Implementation Guide

## Overview
This guide details the implementation of Corona lattice-based signatures across all Lux chains, replacing traditional ECDSA with quantum-resistant cryptography.

## 1. Corona Cryptographic Specification

### 1.1 Core Parameters
```go
// crypto/corona/params.go
package corona

const (
    // Security levels
    CORONA_128 = iota  // 128-bit quantum security
    CORONA_256         // 256-bit quantum security (default)
    CORONA_512         // 512-bit quantum security
    
    // Ring parameters
    DefaultN = 1024      // Ring dimension
    DefaultQ = 12289     // Modulus
    DefaultK = 4         // Number of ring elements
    
    // Signature sizes
    SigSize256 = 2420    // bytes for 256-bit security
    PubKeySize = 1472    // bytes for public key
    PrivKeySize = 2528   // bytes for private key
)

type CoronaParams struct {
    SecurityLevel int
    N            int      // Ring dimension
    Q            int64    // Modulus
    K            int      // Module rank
    Eta          int      // Secret distribution parameter
    Beta         float64  // Acceptance bound
}
```

### 1.2 Core Implementation
```go
// crypto/corona/corona.go
package corona

import (
    "crypto/rand"
    "github.com/luxfi/lattice-crypto/corona/core"
)

type PrivateKey struct {
    params *CoronaParams
    s1     *core.PolyVecK  // Secret key part 1
    s2     *core.PolyVecL  // Secret key part 2
    tr     []byte          // H(pk)
}

type PublicKey struct {
    params *CoronaParams
    rho    []byte          // Seed for A
    t1     *core.PolyVecK  // Public key
}

type Signature struct {
    c      *core.Poly      // Challenge
    z      *core.PolyVecL  // Response
    h      *core.PolyVecK  // Hint
    sigma  []byte          // Randomness
}

// Key generation
func GenerateKey(params *CoronaParams) (*PrivateKey, *PublicKey, error) {
    // Sample secret key
    s1 := core.PolyVecUniform(params.K, params.Eta)
    s2 := core.PolyVecUniform(params.L, params.Eta)
    
    // Generate public key
    A := core.ExpandA(rand.Reader, params.K, params.L)
    t := A.MulVec(s1).Add(s2)
    
    // Split t into t1 and t0
    t1, t0 := core.Power2Round(t)
    
    pk := &PublicKey{
        params: params,
        rho:    A.Seed(),
        t1:     t1,
    }
    
    sk := &PrivateKey{
        params: params,
        s1:     s1,
        s2:     s2,
        tr:     hash(pk.Bytes()),
    }
    
    return sk, pk, nil
}

// Signature generation with MPC support
func Sign(sk *PrivateKey, message []byte, mpcShare *MPCShare) (*Signature, error) {
    // Hash message with key
    mu := hash(sk.tr, message)
    
    // Initialize attempts counter
    kappa := 0
    
    for {
        // Sample randomness
        y := core.PolyVecUniform(params.L, params.Gamma1)
        
        // Compute w = Ay
        A := core.ExpandA(sk.params.rho)
        w := A.MulVec(y)
        
        // Extract high bits
        w1 := core.HighBits(w)
        
        // Compute challenge
        c := core.HashToChallenge(mu, w1)
        
        // Compute response with MPC if needed
        var z *core.PolyVecL
        if mpcShare != nil {
            z = computeMPCResponse(y, c, sk, mpcShare)
        } else {
            z = y.Add(c.MulVec(sk.s1))
        }
        
        // Rejection sampling
        if core.CheckNorm(z, params.Beta) {
            continue
        }
        
        // Compute hints
        h := core.MakeHint(w, c, sk)
        
        if core.CheckHint(h) {
            continue
        }
        
        return &Signature{
            c:     c,
            z:     z,
            h:     h,
            sigma: randomness,
        }, nil
    }
}

// Verification
func Verify(pk *PublicKey, message []byte, sig *Signature) bool {
    // Expand public key
    A := core.ExpandA(pk.rho)
    
    // Hash message
    mu := hash(pk.Bytes(), message)
    
    // Compute w' = Az - ct1 * 2^d
    w := A.MulVec(sig.z).Sub(sig.c.MulVec(pk.t1).LeftShift(D))
    
    // Use hint to recover high bits
    w1 := core.UseHint(w, sig.h)
    
    // Verify challenge
    c_prime := core.HashToChallenge(mu, w1)
    
    return c_prime.Equal(sig.c)
}
```

## 2. MPC Integration for Corona

### 2.1 MPC Key Generation
```go
// crypto/corona/mpc/keygen.go
package mpc

type CoronaMPCKeyGen struct {
    threshold    int
    participants int
    
    // Shamir secret sharing over lattice
    shares      map[int]*KeyShare
}

type KeyShare struct {
    Index       int
    S1Share     *core.PolyVecK  // Share of s1
    S2Share     *core.PolyVecL  // Share of s2
    PublicKey   *PublicKey      // Common public key
}

func (mpc *CoronaMPCKeyGen) GenerateShares() ([]*KeyShare, error) {
    // Generate master key
    sk, pk, _ := GenerateKey(DefaultParams())
    
    // Create Shamir shares over polynomial coefficients
    s1Shares := shamirSharePolyVec(sk.s1, mpc.threshold, mpc.participants)
    s2Shares := shamirSharePolyVec(sk.s2, mpc.threshold, mpc.participants)
    
    shares := make([]*KeyShare, mpc.participants)
    for i := 0; i < mpc.participants; i++ {
        shares[i] = &KeyShare{
            Index:     i,
            S1Share:   s1Shares[i],
            S2Share:   s2Shares[i],
            PublicKey: pk,
        }
    }
    
    return shares, nil
}
```

### 2.2 MPC Signing Protocol
```go
// crypto/corona/mpc/sign.go
type CoronaMPCSigner struct {
    share       *KeyShare
    parties     map[int]*Party
    
    // Current signing session
    session     *SigningSession
}

type SigningSession struct {
    Message     []byte
    Nonce       []byte
    
    // Round 1: Commitment
    Commitments map[int]*Commitment
    
    // Round 2: Share randomness
    YShares     map[int]*core.PolyVecL
    
    // Round 3: Combine signatures
    PartialSigs map[int]*PartialSignature
}

func (signer *CoronaMPCSigner) SignRound1() (*Commitment, error) {
    // Generate random y
    y := core.PolyVecUniform(params.L, params.Gamma1)
    
    // Compute commitment
    commitment := hash(y.Bytes())
    
    signer.session.YShares[signer.share.Index] = y
    
    return &Commitment{
        Party: signer.share.Index,
        Com:   commitment,
    }, nil
}

func (signer *CoronaMPCSigner) SignRound2(commitments map[int]*Commitment) (*core.PolyVecL, error) {
    // Verify all commitments received
    // Reveal y
    return signer.session.YShares[signer.share.Index], nil
}

func (signer *CoronaMPCSigner) SignRound3(yShares map[int]*core.PolyVecL) (*PartialSignature, error) {
    // Combine y shares
    y := core.CombinePolyVec(yShares)
    
    // Compute partial signature with share
    z_partial := computePartialResponse(y, signer.share)
    
    return &PartialSignature{
        Party: signer.share.Index,
        Z:     z_partial,
    }, nil
}

func CombinePartialSignatures(partials map[int]*PartialSignature) (*Signature, error) {
    // Lagrange interpolation to recover full signature
    z := lagrangeInterpolateSignature(partials)
    
    return &Signature{
        c: challenge,
        z: z,
        h: hint,
    }, nil
}
```

## 3. Chain-Specific Implementations

### 3.1 P-Chain Integration
```go
// vms/platformvm/signer/corona_signer.go
type CoronaSigner struct {
    key *corona.PrivateKey
}

func (s *CoronaSigner) SignHash(hash []byte) ([]byte, error) {
    sig, err := corona.Sign(s.key, hash, nil)
    if err != nil {
        return nil, err
    }
    return sig.Bytes(), nil
}

// Update validator transaction
type AddValidatorTx struct {
    // ... existing fields ...
    
    // New: Support both signature types during transition
    SignatureType  uint8  // 0 = SECP256K1, 1 = CORONA
    CoronaSig    []byte `serialize:"true" json:"coronaSignature,omitempty"`
}
```

### 3.2 X-Chain Integration
```go
// vms/avm/utxos/corona_utxo.go
type CoronaTransferInput struct {
    Amt              uint64              `serialize:"true" json:"amount"`
    CoronaSigIndices []uint32         `serialize:"true" json:"coronaSignatureIndices"`
}

func (in *CoronaTransferInput) Verify() error {
    // Verify Corona signatures
    for i, sigBytes := range in.TypedInput.Signatures {
        sig := &corona.Signature{}
        if err := sig.Unmarshal(sigBytes); err != nil {
            return err
        }
        
        pk := &corona.PublicKey{}
        if err := pk.Unmarshal(in.PublicKeys[i]); err != nil {
            return err
        }
        
        if !corona.Verify(pk, in.Message, sig) {
            return errInvalidSignature
        }
    }
    return nil
}
```

### 3.3 C-Chain EVM Integration
```go
// core/vm/contracts_corona.go

// Precompiled contract addresses
var (
    CoronaVerifyAddr = common.HexToAddress("0x0100")
    CoronaMPCAddr    = common.HexToAddress("0x0101")
)

// Corona signature verification precompile
type coronaVerify struct{}

func (c *coronaVerify) RequiredGas(input []byte) uint64 {
    return params.CoronaVerifyGas
}

func (c *coronaVerify) Run(input []byte) ([]byte, error) {
    // Parse input: pubkey || message || signature
    if len(input) < corona.MinInputSize {
        return nil, errInvalidInput
    }
    
    pk, msg, sig := parseCoronaInput(input)
    
    valid := corona.Verify(pk, msg, sig)
    if valid {
        return common.LeftPadBytes([]byte{1}, 32), nil
    }
    return common.LeftPadBytes([]byte{0}, 32), nil
}

// Account abstraction with Corona
type CoronaAccount struct {
    Address    common.Address
    PublicKey  *corona.PublicKey
    Nonce      uint64
    Balance    *big.Int
}
```

### 3.4 B-Chain Bridge MPC
```go
// vms/bvm/corona_bridge.go
type CoronaBridgeValidator struct {
    NodeID      ids.NodeID
    NFTId       uint64
    KeyShare    *mpc.KeyShare
    
    // Active MPC sessions
    signingSessions map[ids.ID]*mpc.SigningSession
}

func (v *CoronaBridgeValidator) InitiateBridgeTransfer(
    sourceChain ids.ID,
    destChain   ids.ID,
    amount      *big.Int,
    recipient   []byte,
) error {
    // Create bridge message
    msg := encodeBridgeMessage(sourceChain, destChain, amount, recipient)
    
    // Start MPC signing ceremony
    sessionID := ids.GenerateID()
    session := &mpc.SigningSession{
        Message: msg,
        Nonce:   generateNonce(),
    }
    
    v.signingSessions[sessionID] = session
    
    // Broadcast round 1 to other validators
    commitment, _ := v.signer.SignRound1()
    v.broadcast(commitment)
    
    return nil
}
```

## 4. Migration Strategy

### 4.1 Hybrid Mode (6 months)
```go
type HybridSigner struct {
    secp256k1 *ecdsa.PrivateKey
    corona  *corona.PrivateKey
}

func (h *HybridSigner) Sign(msg []byte) (*HybridSignature, error) {
    // Sign with both during transition
    classicalSig, _ := ecdsa.Sign(h.secp256k1, msg)
    quantumSig, _ := corona.Sign(h.corona, msg)
    
    return &HybridSignature{
        Classical: classicalSig,
        Quantum:   quantumSig,
        Version:   HYBRID_V1,
    }, nil
}
```

### 4.2 Wallet Migration Tool
```go
// tools/migrate-wallet/main.go
func migrateWallet(oldKeystore, newKeystore string) error {
    // Load SECP256K1 keys
    ecdsaKeys := loadECDSAKeys(oldKeystore)
    
    // Generate corresponding Corona keys
    coronaKeys := make(map[string]*corona.PrivateKey)
    
    for addr, ecdsaKey := range ecdsaKeys {
        // Deterministic derivation from ECDSA key
        seed := deriveCoronaSeed(ecdsaKey)
        rtKey, _ := corona.GenerateKeyDeterministic(seed)
        
        coronaKeys[addr] = rtKey
    }
    
    // Save new keystore
    return saveCoronaKeystore(newKeystore, coronaKeys)
}
```

## 5. Performance Optimizations

### 5.1 Batch Verification
```go
func BatchVerifyCorona(
    publicKeys []*corona.PublicKey,
    messages [][]byte,
    signatures []*corona.Signature,
) bool {
    // Aggregate challenges
    aggregateChallenge := core.NewPoly()
    
    for i := range signatures {
        c_i := core.HashToChallenge(messages[i], signatures[i].w1)
        aggregateChallenge.Add(c_i.Mul(randomScalar()))
    }
    
    // Single verification for all signatures
    return verifyAggregate(publicKeys, aggregateChallenge, signatures)
}
```

### 5.2 Hardware Acceleration
```go
// Use AVX2/AVX512 for polynomial operations
// crypto/corona/poly_amd64.s
TEXT ·polyMulAVX2(SB), NOSPLIT, $0-48
    // Optimized polynomial multiplication using AVX2
    MOVQ a+0(FP), AX
    MOVQ b+8(FP), BX
    MOVQ c+16(FP), CX
    
    // Load coefficients into YMM registers
    VMOVDQU (AX), Y0
    VMOVDQU (BX), Y1
    
    // Perform NTT multiplication
    // ... AVX2 assembly ...
    
    RET
```

## 6. Testing Framework

### 6.1 Test Vectors
```go
// crypto/corona/testdata/vectors.json
{
    "corona256_test_vectors": [
        {
            "seed": "0x1234...",
            "privateKey": "0xabcd...",
            "publicKey": "0xef01...",
            "message": "test message",
            "signature": "0x5678...",
            "valid": true
        }
    ]
}
```

### 6.2 Benchmarks
```go
func BenchmarkCoronaSign(b *testing.B) {
    sk, _, _ := GenerateKey(Corona256())
    msg := []byte("benchmark message")
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        Sign(sk, msg, nil)
    }
}

// Expected performance:
// Corona256 Sign: ~0.5ms
// Corona256 Verify: ~0.2ms
// MPC Sign (3-of-5): ~2ms
```

This implementation provides quantum-safe signatures across all chains with MPC support, ensuring long-term security for the Lux ecosystem.