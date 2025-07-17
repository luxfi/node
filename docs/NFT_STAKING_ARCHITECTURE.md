# NFT Staking Architecture for Lux Validators

## Overview

This document defines the NFT staking mechanism for Lux validators, where NFTs containing LUX can be staked instead of direct LUX staking. NFT ownership or 100,000 LUX stake allows running a Lux mainnet validator and all subnet validators in the Lux ecosystem. Bridge operators require 1,000,000 LUX stake (NFT or direct) and are selected from the top 50% of validators by stake. The bridge operator set can expand or contract daily via epochs, thanks to Ringtail's flexible key management.

## 1. NFT Structure and Design

### 1.1 LUX NFT Specification
```solidity
// contracts/LuxValidatorNFT.sol
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/token/ERC721/ERC721.sol";
import "@openzeppelin/contracts/access/Ownable.sol";

contract LuxValidatorNFT is ERC721, Ownable {
    struct ValidatorNFT {
        uint256 luxAmount;        // Amount of LUX contained in NFT
        uint256 mintTimestamp;    // When NFT was created
        uint256 lastStakeUpdate;  // Last time staking was modified
        bool isStaked;            // Currently staked status
        address stakedValidator;  // Validator address if staked
        uint256 accumulatedRewards; // Rewards earned while staked
        
        // Special attributes
        uint8 tier;               // 0=Standard, 1=Elite, 2=Genesis
        bool isBridgeEligible;    // Can participate in B-Chain
        uint256 bondAmount;       // Additional bond for B-Chain
    }
    
    // NFT metadata
    mapping(uint256 => ValidatorNFT) public nftData;
    
    // Staking requirements
    uint256 public constant MIN_LUX_PER_NFT = 10_000 * 1e18;      // 10,000 LUX minimum
    uint256 public constant STANDARD_NFT_LUX = 100_000 * 1e18;    // 100,000 LUX standard
    uint256 public constant ELITE_NFT_LUX = 500_000 * 1e18;       // 500,000 LUX elite
    uint256 public constant GENESIS_NFT_LUX = 1_000_000 * 1e18;   // 1,000,000 LUX genesis
    
    // Bridge operator thresholds
    uint256 public constant BRIDGE_MIN_STAKE = 1_000_000 * 1e18;  // 1M LUX for bridge eligibility
    
    // Total supply caps
    uint256 public constant MAX_GENESIS_NFTS = 100;   // Top 100 genesis validators
    uint256 public constant MAX_ELITE_NFTS = 900;     // Next 900 elite validators
    uint256 public constant MAX_STANDARD_NFTS = 9000; // Standard validator NFTs
    
    // Counters
    uint256 public genesisCount;
    uint256 public eliteCount;
    uint256 public standardCount;
    
    // LUX token interface
    IERC20 public immutable luxToken;
    
    // Events
    event NFTMinted(uint256 indexed tokenId, address indexed owner, uint256 luxAmount, uint8 tier);
    event NFTStaked(uint256 indexed tokenId, address indexed validator);
    event NFTUnstaked(uint256 indexed tokenId, address indexed validator);
    event RewardsClaimed(uint256 indexed tokenId, uint256 amount);
    event BridgeBondPosted(uint256 indexed tokenId, uint256 bondAmount);
    event BridgeBondSlashed(uint256 indexed tokenId, uint256 slashAmount, string reason);
}
```

### 1.2 NFT Minting and LUX Locking
```solidity
// Mint NFT by locking LUX
function mintValidatorNFT(uint256 luxAmount) external returns (uint256) {
    require(luxAmount >= MIN_LUX_PER_NFT, "Insufficient LUX amount");
    
    // Determine tier based on amount
    uint8 tier;
    if (luxAmount >= GENESIS_NFT_LUX && genesisCount < MAX_GENESIS_NFTS) {
        tier = 2; // Genesis
        genesisCount++;
    } else if (luxAmount >= ELITE_NFT_LUX && eliteCount < MAX_ELITE_NFTS) {
        tier = 1; // Elite
        eliteCount++;
    } else if (standardCount < MAX_STANDARD_NFTS) {
        tier = 0; // Standard
        standardCount++;
    } else {
        revert("NFT supply cap reached");
    }
    
    // Transfer LUX to this contract
    luxToken.transferFrom(msg.sender, address(this), luxAmount);
    
    // Mint NFT
    uint256 tokenId = _nextTokenId++;
    _safeMint(msg.sender, tokenId);
    
    // Store NFT data
    nftData[tokenId] = ValidatorNFT({
        luxAmount: luxAmount,
        mintTimestamp: block.timestamp,
        lastStakeUpdate: 0,
        isStaked: false,
        stakedValidator: address(0),
        accumulatedRewards: 0,
        tier: tier,
        isBridgeEligible: tier >= 1, // Elite and Genesis eligible
        bondAmount: 0
    });
    
    emit NFTMinted(tokenId, msg.sender, luxAmount, tier);
    return tokenId;
}

// Burn NFT to recover LUX
function burnValidatorNFT(uint256 tokenId) external {
    require(ownerOf(tokenId) == msg.sender, "Not NFT owner");
    ValidatorNFT storage nft = nftData[tokenId];
    require(!nft.isStaked, "Cannot burn staked NFT");
    require(nft.bondAmount == 0, "Cannot burn NFT with active bond");
    
    uint256 totalAmount = nft.luxAmount + nft.accumulatedRewards;
    
    // Burn NFT
    _burn(tokenId);
    
    // Update counters
    if (nft.tier == 2) genesisCount--;
    else if (nft.tier == 1) eliteCount--;
    else standardCount--;
    
    // Return LUX to owner
    luxToken.transfer(msg.sender, totalAmount);
    
    delete nftData[tokenId];
}
```

## 2. X-Chain NFT Support

### 2.1 X-Chain NFT UTXO Model
```go
// vms/avm/txs/nft_staking.go
package txs

type NFTStakeUTXO struct {
    UTXOID `serialize:"true" json:"utxoid"`
    Asset  `serialize:"true" json:"asset"`
    
    // NFT-specific data
    NFTData NFTStakeData `serialize:"true" json:"nftData"`
}

type NFTStakeData struct {
    TokenID        uint64            `serialize:"true" json:"tokenId"`
    LuxAmount      uint64            `serialize:"true" json:"luxAmount"`
    Tier           uint8             `serialize:"true" json:"tier"`
    Owner          ids.ShortID       `serialize:"true" json:"owner"`
    ValidatorID    ids.NodeID        `serialize:"true" json:"validatorId,omitempty"`
    IsStaked       bool              `serialize:"true" json:"isStaked"`
    StakeTimestamp uint64            `serialize:"true" json:"stakeTimestamp"`
    Rewards        uint64            `serialize:"true" json:"rewards"`
    
    // Ringtail signature for ownership
    OwnerSig       *ringtail.Signature `serialize:"true" json:"ownerSignature"`
}

// NFT staking transaction
type StakeNFTTx struct {
    BaseTx `serialize:"true"`
    
    // NFT to stake
    NFTInput    NFTStakeUTXO      `serialize:"true" json:"nftInput"`
    
    // Validator to stake to
    ValidatorID ids.NodeID         `serialize:"true" json:"validatorId"`
    
    // Staking duration
    StartTime   uint64             `serialize:"true" json:"startTime"`
    EndTime     uint64             `serialize:"true" json:"endTime"`
    
    // Rewards destination
    RewardsTo   fx.Owner           `serialize:"true" json:"rewardsTo"`
}
```

### 2.2 X-Chain NFT Operations
```go
// vms/avm/nft/operations.go
package nft

type NFTStakingManager struct {
    state       State
    validator   validators.Manager
    rewards     RewardCalculator
}

// Stake NFT to run validator
func (m *NFTStakingManager) StakeNFT(
    nftID ids.ID,
    validatorID ids.NodeID,
    duration time.Duration,
) error {
    // Get NFT UTXO
    nft, err := m.state.GetNFT(nftID)
    if err != nil {
        return err
    }
    
    // Verify ownership
    if !m.verifyNFTOwnership(nft, validatorID) {
        return errNotNFTOwner
    }
    
    // Check if NFT has enough LUX
    if nft.LuxAmount < MinValidatorStake {
        return errInsufficientNFTValue
    }
    
    // Check validator not already staked
    if m.validator.IsStaked(validatorID) {
        return errValidatorAlreadyStaked
    }
    
    // Create staking record
    stake := &NFTStake{
        NFTId:       nftID,
        ValidatorID: validatorID,
        StartTime:   time.Now(),
        EndTime:     time.Now().Add(duration),
        LuxAmount:   nft.LuxAmount,
        Tier:        nft.Tier,
    }
    
    // Update validator set
    if err := m.validator.AddNFTValidator(stake); err != nil {
        return err
    }
    
    // Mark NFT as staked
    nft.IsStaked = true
    nft.ValidatorID = validatorID
    nft.StakeTimestamp = uint64(time.Now().Unix())
    
    return m.state.UpdateNFT(nft)
}

// Calculate and distribute rewards
func (m *NFTStakingManager) CalculateNFTRewards(nftID ids.ID) (uint64, error) {
    nft, err := m.state.GetNFT(nftID)
    if err != nil {
        return 0, err
    }
    
    if !nft.IsStaked {
        return 0, errNFTNotStaked
    }
    
    // Calculate rewards based on:
    // 1. NFT tier (higher tier = more rewards)
    // 2. LUX amount in NFT
    // 3. Staking duration
    // 4. Network performance
    
    baseReward := m.rewards.GetBaseReward()
    tierMultiplier := m.getTierMultiplier(nft.Tier)
    timeStaked := time.Now().Unix() - int64(nft.StakeTimestamp)
    
    reward := uint64(float64(baseReward) * 
                     float64(nft.LuxAmount) / float64(MinValidatorStake) *
                     tierMultiplier *
                     float64(timeStaked) / float64(365*24*3600))
    
    return reward, nil
}

func (m *NFTStakingManager) getTierMultiplier(tier uint8) float64 {
    switch tier {
    case 2: // Genesis
        return 1.5
    case 1: // Elite
        return 1.25
    default: // Standard
        return 1.0
    }
}
```

## 3. Validator Selection Mechanism

### 3.1 NFT vs Direct Staking with Additional Bond
```go
// vms/platformvm/validators/nft_validator.go
package validators

type ValidatorType uint8

const (
    DirectStakeValidator ValidatorType = iota
    NFTStakeValidator
)

type ValidatorEntry struct {
    NodeID      ids.NodeID
    Type        ValidatorType
    
    // For direct stake
    StakedLUX   uint64
    
    // For NFT stake
    NFTId       ids.ID
    NFTTier     uint8
    NFTLuxValue uint64
    
    // Bridge eligibility
    BridgeEligible  bool      // Has 1M+ LUX stake
    BridgeActive    bool      // Currently in bridge operator set
    
    // Common fields
    StartTime   time.Time
    EndTime     time.Time
    Weight      uint64 // Total weight for consensus
}

// Staking requirements
const (
    MinValidatorStake   = 100_000 * units.Lux       // 100K LUX for mainnet & subnet validator
    MinBridgeStake      = 1_000_000 * units.Lux     // 1M LUX for bridge operator
    BridgeOperatorRatio = 0.5                        // Top 50% of validators eligible
)

// Calculate validator weight
func (v *ValidatorEntry) CalculateWeight() uint64 {
    switch v.Type {
    case DirectStakeValidator:
        // Direct stake requires 100K LUX minimum for mainnet
        if v.StakedLUX < MinValidatorStake {
            return 0
        }
        // Check bridge eligibility
        v.BridgeEligible = v.StakedLUX >= MinBridgeStake
        return v.StakedLUX
        
    case NFTStakeValidator:
        // NFT stake gets weight based on contained LUX + tier bonus
        baseWeight := v.NFTLuxValue
        
        // Apply tier bonuses
        switch v.NFTTier {
        case 2: // Genesis NFT
            return baseWeight * 150 / 100 // 50% bonus
        case 1: // Elite NFT
            return baseWeight * 125 / 100 // 25% bonus
        default: // Standard NFT
            return baseWeight
        }
    }
    
    return 0
}

// Validator set manager
type NFTValidatorSet struct {
    validators map[ids.NodeID]*ValidatorEntry
    
    // Separate tracking for eligibility
    directStakers map[ids.NodeID]*ValidatorEntry // Direct LUX stakers
    nftStakers    map[ids.NodeID]*ValidatorEntry // NFT stakers
    
    // Bridge operator management
    bridgeEligible []*ValidatorEntry             // 1M+ LUX validators
    bridgeOperators []*ValidatorEntry             // Active bridge operators (top 50%)
}

// Add validator with NFT
func (vs *NFTValidatorSet) AddNFTValidator(
    nodeID ids.NodeID,
    nftID ids.ID,
    nftData *NFTStakeData,
) error {
    // Verify not already a validator
    if existing, ok := vs.validators[nodeID]; ok {
        return errValidatorAlreadyExists
    }
    
    // Verify NFT has minimum stake for validator
    if nftData.LuxAmount < MinValidatorStake {
        return errInsufficientStake
    }
    
    validator := &ValidatorEntry{
        NodeID:         nodeID,
        Type:           NFTStakeValidator,
        NFTId:          nftID,
        NFTTier:        nftData.Tier,
        NFTLuxValue:    nftData.LuxAmount,
        BridgeEligible: nftData.LuxAmount >= MinBridgeStake,
        BridgeActive:   false,
        StartTime:      time.Now(),
        Weight:         0, // Calculated below
    }
    
    validator.Weight = validator.CalculateWeight()
    
    vs.validators[nodeID] = validator
    vs.nftStakers[nodeID] = validator
    
    // Update B-Chain eligibility
    vs.updateBridgeEligibility()
    
    return nil
}

// Add direct stake validator (requires 100K LUX minimum)
func (vs *NFTValidatorSet) AddDirectStakeValidator(
    nodeID ids.NodeID,
    stakedLUX uint64,
) error {
    // Verify minimum stake for mainnet validator
    if stakedLUX < MinValidatorStake {
        return errInsufficientStake
    }
    
    // Check if already exists
    if existing, ok := vs.validators[nodeID]; ok {
        return errValidatorAlreadyExists
    }
    
    validator := &ValidatorEntry{
        NodeID:         nodeID,
        Type:           DirectStakeValidator,
        StakedLUX:      stakedLUX,
        BridgeEligible: stakedLUX >= MinBridgeStake,
        BridgeActive:   false,
        StartTime:      time.Now(),
        Weight:         stakedLUX,
    }
    
    vs.validators[nodeID] = validator
    vs.directStakers[nodeID] = validator
    
    vs.updateBridgeOperators()
    
    return nil
}

// Update bridge operators based on top 50% of validators
func (vs *NFTValidatorSet) updateBridgeOperators() {
    // Get all bridge-eligible validators (1M+ LUX)
    eligible := make([]*ValidatorEntry, 0)
    for _, v := range vs.validators {
        if v.BridgeEligible {
            eligible = append(eligible, v)
        }
    }
    
    // Sort by stake weight (descending)
    sort.Slice(eligible, func(i, j int) bool {
        return eligible[i].Weight > eligible[j].Weight
    })
    
    // Select top 50% as bridge operators
    operatorCount := len(eligible) / 2
    if operatorCount < 1 && len(eligible) > 0 {
        operatorCount = 1 // At least one operator if any eligible
    }
    
    // Clear previous active status
    for _, v := range vs.bridgeOperators {
        v.BridgeActive = false
    }
    
    // Set new bridge operators
    vs.bridgeOperators = make([]*ValidatorEntry, 0, operatorCount)
    for i := 0; i < operatorCount && i < len(eligible); i++ {
        eligible[i].BridgeActive = true
        vs.bridgeOperators = append(vs.bridgeOperators, eligible[i])
    }
}
```

### 3.2 B-Chain Bridge Operator Selection with Epochs
```go
// vms/bvm/validators/bridge_selection.go
package validators

type BridgeValidatorSelector struct {
    validatorSet *NFTValidatorSet
    bondManager  *BondManager
    
    // Epoch management for Ringtail key stability
    currentEpoch    *BridgeEpoch
    nextEpoch       *BridgeEpoch
    epochDuration   time.Duration // 24 hours default
    
    // Top 1000 validators by stake (1M+ LUX required)
    eligibleValidators []*BridgeValidator
    
    // Active bridge validators (bonded)
    activeValidators map[ids.NodeID]*BridgeValidator
}

type BridgeEpoch struct {
    EpochID        uint64
    StartTime      time.Time
    EndTime        time.Time
    ValidatorSet   []*BridgeValidator
    RingtailKeys   *RingtailKeySet
    TransitionType EpochTransition
}

type EpochTransition uint8

const (
    NoChange EpochTransition = iota
    ValidatorAdded
    ValidatorRemoved
    ValidatorReplaced
    KeyRotation
)

type BridgeValidator struct {
    ValidatorEntry
    
    // Bridge operation tracking
    EpochsActive   uint64
    LastActive     time.Time
    Performance    float64 // Performance score 0-1
    SlashingEvents []SlashingEvent
}

// Check if new epoch needed (called periodically)
func (s *BridgeValidatorSelector) CheckEpochTransition() error {
    now := time.Now()
    
    // Still in current epoch
    if now.Before(s.currentEpoch.EndTime) {
        return nil
    }
    
    // Time for new epoch - check if changes needed
    newValidatorSet := s.calculateNewValidatorSet()
    
    // Compare with current set
    transition := s.determineTransitionType(s.currentEpoch.ValidatorSet, newValidatorSet)
    
    if transition == NoChange {
        // Extend current epoch
        s.currentEpoch.EndTime = now.Add(s.epochDuration)
        return nil
    }
    
    // Create new epoch with key rotation if needed
    return s.createNewEpoch(newValidatorSet, transition)
}

// Calculate eligible bridge operators (top 50% of 1M+ LUX validators)
func (s *BridgeValidatorSelector) calculateNewValidatorSet() []*BridgeValidator {
    // Get all validators with sufficient stake
    eligibleValidators := make([]*ValidatorEntry, 0)
    
    for _, v := range s.validatorSet.validators {
        totalStake := s.calculateTotalStake(v)
        if totalStake >= MinBridgeStake {
            eligibleValidators = append(eligibleValidators, v)
        }
    }
    
    // Sort by total stake weight (descending)
    sort.Slice(eligibleValidators, func(i, j int) bool {
        return eligibleValidators[i].Weight > eligibleValidators[j].Weight
    })
    
    // Select top 50% (flexible size thanks to Ringtail)
    operatorCount := len(eligibleValidators) / 2
    if operatorCount < 1 && len(eligibleValidators) > 0 {
        operatorCount = 1 // At least one operator
    }
    
    bridgeValidators := make([]*BridgeValidator, 0)
    for i := 0; i < operatorCount; i++ {
        bridgeValidators = append(bridgeValidators, &BridgeValidator{
            ValidatorEntry: *eligibleValidators[i],
        })
    }
    
    return bridgeValidators
}

// Calculate total stake including NFT value
func (s *BridgeValidatorSelector) calculateTotalStake(v *ValidatorEntry) uint64 {
    switch v.Type {
    case DirectStakeValidator:
        return v.StakedLUX
    case NFTStakeValidator:
        return v.NFTLuxValue
    }
    return 0
}

// Create new epoch with Ringtail key management
func (s *BridgeValidatorSelector) createNewEpoch(
    newValidatorSet []*BridgeValidator,
    transition EpochTransition,
) error {
    epoch := &BridgeEpoch{
        EpochID:        s.currentEpoch.EpochID + 1,
        StartTime:      s.currentEpoch.EndTime,
        EndTime:        s.currentEpoch.EndTime.Add(s.epochDuration),
        ValidatorSet:   newValidatorSet,
        TransitionType: transition,
    }
    
    // Handle Ringtail key updates based on transition
    switch transition {
    case ValidatorAdded, ValidatorRemoved, ValidatorReplaced:
        // Need new Ringtail DKG ceremony
        keys, err := s.performRingtailDKG(newValidatorSet)
        if err != nil {
            return err
        }
        epoch.RingtailKeys = keys
        
    case KeyRotation:
        // Scheduled key rotation
        keys, err := s.rotateRingtailKeys(s.currentEpoch.RingtailKeys, newValidatorSet)
        if err != nil {
            return err
        }
        epoch.RingtailKeys = keys
    }
    
    // Announce epoch transition
    s.announceEpochTransition(epoch)
    
    // Schedule activation
    s.nextEpoch = epoch
    
    return nil
}

// Activate bridge validator by posting bond
func (s *BridgeValidatorSelector) PostBond(
    validatorID ids.NodeID,
    amount uint64,
    currency string,
) error {
    // Check eligibility
    var eligible *BridgeValidator
    for _, v := range s.eligibleValidators {
        if v.NodeID == validatorID {
            eligible = v
            break
        }
    }
    
    if eligible == nil {
        return errNotEligibleForBridge
    }
    
    // Verify bond amount ($100,000 worth)
    if !s.bondManager.VerifyBondValue(amount, currency) {
        return errInsufficientBond
    }
    
    // Lock bond
    if err := s.bondManager.LockBond(validatorID, amount, currency); err != nil {
        return err
    }
    
    // Activate validator
    eligible.BondAmount = amount
    eligible.BondCurrency = currency
    eligible.BondTimestamp = time.Now()
    
    s.activeValidators[validatorID] = eligible
    
    return nil
}

// Slash validator bond for misbehavior
func (s *BridgeValidatorSelector) SlashValidator(
    validatorID ids.NodeID,
    slashAmount uint64,
    reason string,
) error {
    validator, ok := s.activeValidators[validatorID]
    if !ok {
        return errNotActiveBridgeValidator
    }
    
    // Record slashing event
    event := SlashingEvent{
        Timestamp: time.Now(),
        Amount:    slashAmount,
        Reason:    reason,
    }
    
    validator.SlashingEvents = append(validator.SlashingEvents, event)
    
    // Slash bond
    if err := s.bondManager.SlashBond(validatorID, slashAmount); err != nil {
        return err
    }
    
    // Remove if bond depleted
    if validator.BondAmount <= slashAmount {
        delete(s.activeValidators, validatorID)
    } else {
        validator.BondAmount -= slashAmount
    }
    
    return nil
}
```

## 4. NFT Transfer and Key Rotation

### 4.1 NFT Transfer with Validator Key Rotation
```solidity
// contracts/ValidatorKeyRotation.sol
contract ValidatorKeyRotation {
    // Mapping of NFT ID to current validator keys
    mapping(uint256 => ValidatorKeys) public validatorKeys;
    
    struct ValidatorKeys {
        address nodeOperator;
        bytes32 p2pPublicKey;
        bytes32 consensusPublicKey;
        uint256 lastRotation;
    }
    
    // NFT transfer triggers key rotation
    function onNFTTransfer(
        uint256 tokenId,
        address from,
        address to
    ) external onlyNFTContract {
        // If NFT is staked, require key rotation
        ValidatorNFT memory nft = getNFTData(tokenId);
        
        if (nft.isStaked) {
            // Emit key rotation event
            emit KeyRotationRequired(tokenId, from, to);
            
            // Mark validator for key update
            validatorKeys[tokenId].lastRotation = block.timestamp;
            
            // For B-Chain validators, initiate MPC key reshare
            if (nft.isBridgeEligible && isBridgeActive(tokenId)) {
                initiateMPCKeyReshare(tokenId, to);
            }
        }
    }
    
    // New owner must provide new keys within grace period
    function updateValidatorKeys(
        uint256 tokenId,
        bytes32 newP2PKey,
        bytes32 newConsensusKey,
        bytes calldata signature
    ) external {
        require(ownerOf(tokenId) == msg.sender, "Not NFT owner");
        require(
            block.timestamp <= validatorKeys[tokenId].lastRotation + KEY_ROTATION_GRACE_PERIOD,
            "Grace period expired"
        );
        
        // Verify signature proves control of new keys
        require(verifyKeyOwnership(msg.sender, newP2PKey, newConsensusKey, signature));
        
        // Update keys
        validatorKeys[tokenId] = ValidatorKeys({
            nodeOperator: msg.sender,
            p2pPublicKey: newP2PKey,
            consensusPublicKey: newConsensusKey,
            lastRotation: block.timestamp
        });
        
        emit ValidatorKeysUpdated(tokenId, newP2PKey, newConsensusKey);
    }
}
```

### 4.2 B-Chain MPC Key Reshare on NFT Transfer
```go
// vms/bvm/mpc/key_reshare.go
package mpc

type KeyReshareManager struct {
    mpcManager     *GlobalMPCManager
    nftTracker     *NFTTracker
    validatorSet   *ValidatorSet
}

// Initiate key reshare when B-Chain NFT transferred
func (m *KeyReshareManager) InitiateKeyReshare(
    nftID uint64,
    oldOwner ids.ShortID,
    newOwner ids.ShortID,
) error {
    // Get current validator info
    validator := m.validatorSet.GetValidatorByNFT(nftID)
    if validator == nil {
        return errValidatorNotFound
    }
    
    // Check if validator is active in B-Chain
    if !validator.IsBridgeActive() {
        return nil // No reshare needed
    }
    
    // Create reshare ceremony
    ceremony := &ReshareCeremony{
        ID:          ids.GenerateID(),
        NFTId:       nftID,
        OldOwner:    oldOwner,
        NewOwner:    newOwner,
        StartTime:   time.Now(),
        Protocol:    validator.MPCProtocol,
        Threshold:   m.mpcManager.GetThreshold(),
        Parties:     m.mpcManager.GetActiveParties(),
    }
    
    // Start reshare protocol
    switch ceremony.Protocol {
    case PROTOCOL_CGGMP21:
        return m.reshareCGGMP21(ceremony)
    case PROTOCOL_RINGTAIL:
        return m.reshareRingtail(ceremony)
    case PROTOCOL_HYBRID:
        return m.reshareHybrid(ceremony)
    }
    
    return errUnknownProtocol
}

// CGGMP21 key reshare
func (m *KeyReshareManager) reshareCGGMP21(ceremony *ReshareCeremony) error {
    // Phase 1: Freeze signing with old key
    m.mpcManager.FreezeKey(ceremony.NFTId)
    
    // Phase 2: Generate new shares for new owner
    newShares, err := m.mpcManager.cggmp21.GenerateNewShares(
        ceremony.NewOwner,
        ceremony.Threshold,
        ceremony.Parties,
    )
    if err != nil {
        return err
    }
    
    // Phase 3: Distribute shares to parties
    for partyID, share := range newShares {
        if err := m.sendNewShare(partyID, share); err != nil {
            return err
        }
    }
    
    // Phase 4: Verify new key matches old public key
    newPubKey := m.mpcManager.cggmp21.CombinePublicKey(newShares)
    oldPubKey := m.mpcManager.GetPublicKey(ceremony.NFTId)
    
    if !bytes.Equal(newPubKey, oldPubKey) {
        return errKeyMismatch
    }
    
    // Phase 5: Activate new key
    m.mpcManager.ActivateNewKey(ceremony.NFTId, newShares)
    
    return nil
}
```

## 5. NFT Marketplace Integration

### 5.1 NFT Trading with Staking Status
```solidity
// contracts/NFTMarketplace.sol
contract LuxNFTMarketplace {
    // Listing structure
    struct NFTListing {
        uint256 tokenId;
        address seller;
        uint256 price;      // In LUX
        bool isStaked;      // If currently staking
        uint256 stakingEnds; // When staking period ends
        uint256 accumulatedRewards; // Rewards to date
    }
    
    mapping(uint256 => NFTListing) public listings;
    
    // List NFT for sale (can list while staked)
    function listNFT(
        uint256 tokenId,
        uint256 price
    ) external {
        require(nftContract.ownerOf(tokenId) == msg.sender, "Not owner");
        
        ValidatorNFT memory nft = nftContract.getNFTData(tokenId);
        
        listings[tokenId] = NFTListing({
            tokenId: tokenId,
            seller: msg.sender,
            price: price,
            isStaked: nft.isStaked,
            stakingEnds: nft.isStaked ? getStakingEndTime(tokenId) : 0,
            accumulatedRewards: nft.accumulatedRewards
        });
        
        emit NFTListed(tokenId, price, nft.isStaked);
    }
    
    // Buy NFT (transfer includes validator rights)
    function buyNFT(uint256 tokenId) external payable {
        NFTListing memory listing = listings[tokenId];
        require(listing.price > 0, "Not for sale");
        require(msg.value >= listing.price, "Insufficient payment");
        
        // Transfer NFT
        nftContract.safeTransferFrom(listing.seller, msg.sender, tokenId);
        
        // If staked, initiate key rotation
        if (listing.isStaked) {
            keyRotation.onNFTTransfer(tokenId, listing.seller, msg.sender);
        }
        
        // Transfer payment
        payable(listing.seller).transfer(listing.price);
        
        // Clear listing
        delete listings[tokenId];
        
        emit NFTSold(tokenId, listing.seller, msg.sender, listing.price);
    }
}
```

## 6. Implementation Timeline

### Phase 1: NFT Contract Development (Week 1-2)
- [ ] Deploy LuxValidatorNFT contract on C-Chain
- [ ] Implement minting with LUX locking
- [ ] Add tier system (Standard/Elite/Genesis)
- [ ] Create burn mechanism

### Phase 2: X-Chain Integration (Week 3-4)
- [ ] Add NFT UTXO support to X-Chain
- [ ] Implement NFT staking transactions
- [ ] Create reward calculation system
- [ ] Add NFT transfer operations

### Phase 3: Validator Selection (Week 5-6)
- [ ] Update validator set to support NFTs
- [ ] Implement 1M LUX vs NFT requirement
- [ ] Create weight calculation system
- [ ] Build top 1000 selection mechanism

### Phase 4: B-Chain Integration (Week 7-8)
- [ ] Add bond posting mechanism
- [ ] Implement slashing conditions
- [ ] Create eligibility tracking
- [ ] Build bond management system

### Phase 5: Key Rotation (Week 9-10)
- [ ] Implement NFT transfer hooks
- [ ] Create key rotation protocol
- [ ] Add MPC key reshare for B-Chain
- [ ] Build grace period system

### Phase 6: Marketplace (Week 11-12)
- [ ] Deploy marketplace contract
- [ ] Add staked NFT trading
- [ ] Implement automated key rotation
- [ ] Create reward distribution

## 7. Security Considerations

### 7.1 NFT Security
- NFTs are non-fungible and unique
- LUX locked in NFT contract is secure
- Only NFT owner can stake/unstake
- Burning requires unstaked status

### 7.2 Validator Security
- One validator per address (NFT or 1M LUX)
- Automatic key rotation on transfer
- Grace period for key updates
- Slashing for misbehavior

### 7.3 B-Chain Security
- $100,000 bond requirement
- Bond can be slashed for misbehavior
- Only top 1000 validators eligible
- MPC key reshare on NFT transfer

## 8. Economic Model

### 8.1 NFT Value Proposition
```
Standard NFT (100,000 LUX):
- Run mainnet validator
- Run all subnet validators
- Earn staking rewards
- Tradeable asset

Elite NFT (500,000 LUX):
- 25% reward bonus
- Higher trading value
- Limited supply (900)

Genesis NFT (1,000,000 LUX):
- 50% reward bonus
- Bridge operator eligible
- Priority in top 50% selection
- Highest trading value
- Ultra-limited (100)
```

### 8.2 Reward Distribution
```
Base Annual Reward: 5% of staked LUX

Validator Rewards (100K+ LUX):
NFT Staking:
- Standard (100K): 5% APY
- Elite (500K): 6.25% APY (25% bonus)
- Genesis (1M): 7.5% APY (50% bonus)

Direct Staking:
- 100K-999K LUX: 5% APY
- 1M+ LUX: 5% APY + Bridge eligibility

Bridge Operators (Top 50% of 1M+ validators):
- Base validator rewards
- Additional bridge operation fees
- Share of cross-chain transaction revenue
- Dynamic set size based on network needs
```

This architecture enables flexible validator participation through NFTs while maintaining network security and creating a liquid market for validator positions. The simplified staking model (100K for validators, 1M for bridge operators) combined with dynamic bridge operator selection (top 50%) ensures both accessibility and security for the Lux ecosystem.