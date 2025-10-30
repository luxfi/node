# Lux Network New Chain Implementations

This directory contains the implementation of the new chains for Lux Network's 6-chain architecture (P, X, C, A, B, Z).

## A-Chain: Attestation & Oracle Co-Processor Chain

Location: `/vms/attestationvm/`

The A-Chain is designed for AI computation verification, oracle attestations, and data verification using threshold signatures.

### Key Features:
- **Attestation Types**: Oracle data, TEE attestations, GPU computation proofs
- **Threshold Signatures**: Support for CGGMP21-style threshold ECDSA and BLS aggregate signatures
- **Oracle Registry**: Manages registered oracle providers with reputation tracking
- **Flexible Verification**: Supports multiple attestation types and proof systems

### Components:
- `vm.go` - Main VM implementation
- `attestation.go` - Attestation types and structures
- `block.go` - Block structure for attestation chain
- `signature.go` - Threshold signature verification (preparing for CGGMP21)
- `attestation_db.go` - Database for managing attestations
- `oracle_registry.go` - Registry for oracle providers
- `handlers.go` - HTTP RPC endpoints

### Usage:
```go
// Submit an attestation
attestation := &Attestation{
    Type:       AttestationTypeOracle,
    SourceID:   "oracle1",
    Data:       priceData,
    Signatures: thresholdSigs,
    SignerIDs:  signerIDs,
}
vm.SubmitAttestation(attestation)
```

## Z-Chain: Confidential UTXO Chain

Location: `/vms/zkutxovm/`

The Z-Chain implements a privacy-focused blockchain using zero-knowledge proofs and optional fully homomorphic encryption.

### Key Features:
- **Confidential Transactions**: Encrypted amounts with ZK proofs
- **Multiple Proof Systems**: Groth16, PLONK, Bulletproofs
- **Nullifier-based Double-Spend Prevention**: Similar to Zcash
- **Private Addresses**: Viewing keys for transaction scanning
- **Optional FHE**: Support for computations on encrypted data
- **UTXO Model**: Privacy-preserving UTXO with commitments

### Components:
- `vm.go` - Main VM implementation
- `transaction.go` - Shielded transaction structure
- `block.go` - Block structure with state root
- `utxo_db.go` - UTXO set management
- `nullifier_db.go` - Nullifier tracking
- `proof_verifier.go` - ZK proof verification (Groth16, PLONK, etc.)
- `state_tree.go` - Merkle tree of UTXO set
- `mempool.go` - Transaction pool management
- `fhe_processor.go` - FHE operations (optional)
- `address_manager.go` - Private address generation and management
- `handlers.go` - HTTP RPC endpoints

### Transaction Types:
1. **Transfer**: Fully shielded transfers
2. **Shield**: Convert transparent to shielded
3. **Unshield**: Convert shielded to transparent
4. **Mint/Burn**: Token issuance and destruction

### Usage:
```go
// Create a shielded transaction
tx := &Transaction{
    Type:       TransactionTypeTransfer,
    Nullifiers: [][]byte{nullifier1, nullifier2},
    Outputs:    []*ShieldedOutput{output1, output2},
    Proof:      zkProof,
}

// Generate a private address
addr, err := vm.addressManager.GenerateAddress()
// addr.Address - public address for receiving
// addr.ViewingKey - for scanning blockchain
// addr.SpendingKey - for spending (keep private!)
```

## B-Chain: Bridge/Beacon Chain

Location: `/vms/bridgevm/` (previously implemented)

The B-Chain handles cross-chain interoperability with enhanced security through the upgraded threshold signatures.

### Key Features:
- Ethereum light client integration
- Atomic cross-chain transactions
- 100M LUX requirement for bridge validators
- Will use CGGMP21 threshold signatures

## Integration with Existing Chains

### P-Chain (Platform)
- NFT-based validator staking
- 1M LUX minimum stake (or equivalent with NFT multipliers)
- Manages validators for all chains

### X-Chain (Exchange)
- Asset transfers and trading
- NFT minting for validator qualification

### C-Chain (Contract)
- Smart contract platform
- NFT contracts for validator staking

## Token Economics

- **Total Supply**: 2 trillion LUX
- **Validator Requirements**:
  - Standard: 1M LUX minimum
  - Bridge (B-Chain): 100M LUX
  - NFT holders get multipliers reducing requirement
- **Native Token**: LUX (not REQL)

## Next Steps

1. **CGGMP21 Integration**: Upgrade threshold signatures across all chains
2. **Testing**: Comprehensive test suite for new features
3. **NFT Contract**: Deploy validator NFT on C-Chain
4. **Network Launch**: 5-node test network with all 6 chains

## Security Considerations

- All chains use threshold signatures for enhanced security
- Z-Chain provides optional privacy with ZK proofs
- A-Chain enables verified computation and oracle data
- B-Chain secures cross-chain assets with high staking requirements