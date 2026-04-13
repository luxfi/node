# Security

## Reporting Vulnerabilities

Report security issues to **security@lux.network**. Do not open public issues for vulnerabilities.

- Provide a description, reproduction steps, and affected components.
- We will acknowledge receipt within 48 hours.
- We will provide an initial assessment within 7 business days.
- We coordinate disclosure timelines with the reporter.

If the vulnerability affects production funds or consensus safety, we treat it as P0 and begin remediation immediately.

## Cryptographic Primitives

Production implementations live in `lux/crypto/` and `lux/lattice/`. Formal verification proofs for each primitive are in `lux/papers/proofs/`.

### Signatures

| Primitive | Standard | Implementation | Use |
|-----------|----------|---------------|-----|
| BLS12-381 | draft-irtf-cfrg-bls-signature | `crypto/bls/` | Validator consensus, warp message aggregation |
| ECDSA secp256k1 | SEC 2 | `crypto/secp256k1/` | EVM transaction signing, C-Chain |
| ECDSA secp256r1 | FIPS 186-5 | `crypto/secp256r1/` | WebAuthn, hardware key support |
| ML-DSA-65 | FIPS 204 | `crypto/mldsa/` | Post-quantum validator identity |
| SLH-DSA | FIPS 205 | `crypto/slhdsa/` | Hash-based PQ fallback signatures |
| Falcon-512/1024 | NIST Round 3 | `crypto/pq/` | EVM precompile PQ signatures (ETHFALCON) |
| Corona | Internal | `lux/lattice/` | Lattice-based threshold signatures for anonymous validator participation |

### Key Encapsulation

| Primitive | Standard | Implementation | Use |
|-----------|----------|---------------|-----|
| ML-KEM-768 | FIPS 203 | `crypto/mlkem/` | Post-quantum key exchange, encrypted P2P handshake |
| HPKE | RFC 9180 | `crypto/hpke/` | Hybrid public key encryption |

### Symmetric and AEAD

| Primitive | Standard | Implementation | Use |
|-----------|----------|---------------|-----|
| ChaCha20-Poly1305 | RFC 8439 | `crypto/aead/` | Authenticated encryption for P2P transport |
| AES-256-GCM | NIST SP 800-38D | `crypto/aead/` | Alternative AEAD for hardware-accelerated paths |

### Key Derivation and Hashing

| Primitive | Standard | Implementation | Use |
|-----------|----------|---------------|-----|
| Argon2id | RFC 9106 | `crypto/kdf/` | Password hashing, key stretching |
| HKDF-SHA256 | RFC 5869 | `crypto/kdf/` | Key derivation from shared secrets |
| Keccak-256 | FIPS 202 | `crypto/keccak.go` | EVM address derivation, state hashing |
| BLAKE2b | RFC 7693 | `crypto/blake2b/` | Non-EVM hashing, content addressing |
| Poseidon2 | ZK-friendly | `crypto/hash/` | Zero-knowledge circuit hashing (Z-Chain) |

### Threshold and MPC

| Primitive | Protocol | Implementation | Use |
|-----------|----------|---------------|-----|
| FROST | Komlo-Goldberg 2020 | `crypto/threshold/` | Threshold Schnorr signatures for bridge custody |
| CGGMP21 | Canetti et al. 2021 | `crypto/cggmp21/` | Threshold ECDSA for multi-chain custody |
| LSS | Shamir + live resharing | `crypto/secret/` | Dynamic secret sharing with participant rotation |

### Fully Homomorphic Encryption

| Primitive | Scheme | Implementation | Use |
|-----------|--------|---------------|-----|
| TFHE | Torus FHE | `crypto/` + precompiles | Encrypted smart contract computation |
| CKKS | Approximate arithmetic | `crypto/` | Privacy-preserving ML inference |

## Network Security

### P2P Transport

- All peer connections use mutual TLS 1.3.
- Post-quantum handshake option via ML-KEM-768 + X25519 hybrid key exchange (`crypto/kem/`).
- Peer identity bound to staking key (BLS public key for validators, secp256k1 for API nodes).
- Eclipse resistance via peer discovery protocol with formal proof (`papers/proofs/proof-network-peer-discovery.tex`).

### Consensus Transport

- ZAP binary wire protocol (`papers/lux-zap-wire-protocol.tex`) for consensus messages.
- Zero-allocation serialization path -- no GC pressure under load.
- Warp messaging for cross-chain: BLS aggregate signatures verified on-chain (`papers/lux-warp-messaging.tex`).

### Validator Security

- Zero-trust validator architecture (`papers/lux-zero-trust-validators.tex`).
- HSM boundary design for validator keys (`papers/lux-hsm-boundary.tex`).
- Hybrid certificate chains with PQ trust anchors (`papers/lux-hybrid-certificates.tex`).
- Reproducible builds with content-addressed attestation (`papers/lux-reproducible-builds.tex`).

## Key Management

### Validator Keys

- BLS signing keys stored in HSM (PKCS#11) or secure enclave where available.
- Threshold key generation via DKG -- no single party holds the full key.
- Key rotation via live secret resharing (LSS protocol) without chain downtime.

### HD Wallets

- BIP-32/44 hierarchical deterministic derivation.
- secp256k1 and secp256r1 key paths.
- Hardware wallet integration (Ledger, Trezor) for end-user keys.

### MPC Custody (M-Chain)

- FROST t-of-n for Schnorr/Taproot custody.
- CGGMP21 t-of-n for ECDSA custody (Ethereum, Bitcoin legacy).
- Session lifecycle management with NATS transport.
- Formal proofs: `papers/proofs/proof-crypto-frost.tex`, `papers/proofs/proof-crypto-cggmp21.tex`.

### Bridge Custody

- Teleport bridge uses MPC group keys -- no single custodian.
- Per-chain governance: each chain's bridge parameters are sovereign.
- Configurable key rotation delay.
- Formal proof: `papers/proofs/proof-bridge-teleport.tex`.

## Audit History

### Round 1 -- December 2025 (Component Audits)

3 targeted audits covering DexVM, oracle protocol, and perpetuals contracts:

| Report | Scope |
|--------|-------|
| `audits/2025-12-11-dexvm-audit.md` | DEX VM code review |
| `audits/2025-12-11-oracle-audit.md` | Oracle and price feed implementation |
| `audits/2025-12-11-perpetuals-audit.md` | Perpetuals and derivatives contracts |

### Round 2 -- December 2025 (Full Ecosystem)

12 component audits covering the entire node implementation. Compiled from commit `66d514d2b7`.

| Report | Scope |
|--------|-------|
| `audits/2025-12-30-architecture-review.md` | Full architecture review |
| `audits/2025-12-30-consensus-audit.md` | Consensus layer (Snow, Quasar, DAG) |
| `audits/2025-12-30-contracts-audit.md` | Smart contract security |
| `audits/2025-12-30-crypto-audit.md` | Cryptography stack (BLS, PQ, MPC) |
| `audits/2025-12-30-database-audit.md` | Storage layer |
| `audits/2025-12-30-dexvm-audit.md` | DexVM (D-Chain) |
| `audits/2025-12-30-network-audit.md` | Network layer and P2P |
| `audits/2025-12-30-oracle-protocol-audit.md` | Oracle and attestation protocol |
| `audits/2025-12-30-other-vms-audit.md` | Secondary VMs |
| `audits/2025-12-30-platformvm-audit.md` | PlatformVM (P-Chain) |
| `audits/2025-12-30-proposervm-evm-audit.md` | ProposerVM and EVM integration |
| `audits/2025-12-30-thresholdvm-audit.md` | ThresholdVM (T-Chain) |
| `audits/2025-12-30-warp-audit.md` | Warp cross-chain messaging |
| `audits/2025-12-30-zkvm-audit.md` | ZKVM (Z-Chain) |

Summary report: `security/2025-12-30-final-security-analysis.md`
Status report: `security/2025-12-30-FINAL-STATUS.md`

164 total findings (17 critical, 42 high, 58 medium, 47 low). Identified development placeholders (XOR stubs, length-only verification) in advanced features not yet in production. Core chains (P-Chain, X-Chain, C-Chain) passed clean.

### Round 3 -- January/March 2026 (Smart Contracts)

Two focused audits on the Solidity contract stack:

| Report | Scope |
|--------|-------|
| `audits/standard-2026-01-30/` | `@luxfi/standard` contract suite -- 832 tests, 105 fuzz tests |
| `audits/2026-03-25-comprehensive-security-audit.md` | `lux/standard` v1.6.5, `lux/liquid` v1.1.0, `liquidity/contracts` |

The March 2026 comprehensive audit used red/blue adversarial methodology with Foundry, Slither, Semgrep, Aderyn, Halmos (symbolic execution), and Lean 4 (theorem proving).

Results: 15 critical, 13 high, 10 medium, 3 low -- all remediated. 1,383 tests passing. 48 Halmos symbolic proofs + 33 Lean 4 theorems + 33 Foundry invariant tests.

Post-remediation risk: LOW. CI enforces Slither (fail-on: medium), Semgrep, Aderyn, and `forge fmt` on every push to main.

### Current Status

All critical and high findings from the contract audits are resolved. The December 2025 node audit identified development stubs in post-quantum and zero-knowledge subsystems that are not deployed to production; these are tracked and being replaced with real implementations as each subsystem matures.

## Formal Verification

50 mechanized proofs in `papers/proofs/`, covering:

- **Consensus**: safety, liveness, BFT thresholds, finality composition, validator economics
- **Cryptography**: BLS aggregation, FROST unforgeability, CGGMP21 UC-security, ML-DSA, ML-KEM, SLH-DSA, Corona, TFHE, CKKS, Verkle commitments, hybrid signatures, threshold composition, linear secret sharing
- **DeFi**: AMM invariants, order book correctness, flash loan safety, router correctness, governance, fee models
- **Bridge**: Teleport protocol, warp message security/delivery/ordering
- **Network**: peer discovery and eclipse resistance
- **Build**: reproducibility, attestation, coeffect algebra, cross-ecosystem verification
- **Trust**: authority lattice, vouch model, revocation

See `papers/INDEX.md` for the full list.

## Bug Bounty

If you discover a vulnerability, contact **security@lux.network**. We will work with you on responsible disclosure and appropriate recognition.

---

*Lux Industries -- security@lux.network*
