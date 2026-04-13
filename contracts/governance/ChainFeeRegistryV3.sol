// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/AccessControl.sol";
import "@openzeppelin/contracts/utils/ReentrancyGuard.sol";

/**
 * @title ChainFeeRegistryV3
 * @notice EIP-1559 style fee model adapted for Lux multi-chain architecture
 * @dev Implements LP-9020 Fee Pricing Protocol with per-unit pricing
 *
 * Key Concepts:
 *   - "Fee-units" = w(tx) = pByte × bytes + pExec × exec + pState × state
 *   - basePerUnit: Protocol-determined base rate (µLUX per fee-unit)
 *   - Txs specify maxFeePerUnit and maxPriorityFeePerUnit (like EIP-1559)
 *   - effectiveTip = min(maxPriorityFee, maxFee - baseFee)
 *   - totalPaid = w(tx) × (basePerUnit + effectiveTipPerUnit)
 *
 * Fee Distribution:
 *   - Base fee: Burned or routed to treasury (configurable split)
 *   - Priority fee: Goes to validators/sequencers
 */
contract ChainFeeRegistryV3 is AccessControl, ReentrancyGuard {

    bytes32 public constant GOVERNOR_ROLE = keccak256("GOVERNOR_ROLE");
    bytes32 public constant EMERGENCY_ROLE = keccak256("EMERGENCY_ROLE");
    bytes32 public constant FEE_UPDATER_ROLE = keccak256("FEE_UPDATER_ROLE");

    // Timelock configuration
    uint256 public constant TIMELOCK_DELAY = 48 hours;

    struct PendingConfigChange {
        uint8 chainId;
        ChainFeeConfig config;
        uint256 executeAfter;
        bool exists;
    }

    struct PendingDistributionChange {
        uint8 chainId;
        FeeDistribution distribution;
        uint256 executeAfter;
        bool exists;
    }

    mapping(bytes32 => PendingConfigChange) public pendingConfigChanges;
    mapping(bytes32 => PendingDistributionChange) public pendingDistributionChanges;

    // ============================================================
    // Fee Parameters (per chain)
    // ============================================================

    /// @notice Weight coefficients for fee-unit calculation
    struct WeightCoefficients {
        uint64 pByteMicroLux;     // Per-byte weight (µLUX per byte)
        uint64 pExecMicroLux;     // Per-exec-unit weight (µLUX per unit)
        uint64 pStateMicroLux;    // Per-state-touch weight (µLUX per touch)
    }

    /// @notice EIP-1559 style base fee parameters
    struct BaseFeeParams {
        uint64 basePerUnit;           // Current base fee per fee-unit (µLUX)
        uint64 minBasePerUnit;        // Floor (prevents base from going to 0)
        uint64 maxBasePerUnit;        // Ceiling (prevents runaway fees)
        uint32 targetUtilization;     // Target block utilization (basis points, 5000 = 50%)
        uint32 maxChangePerBlock;     // Max % change per block (basis points, 1250 = 12.5%)
    }

    /// @notice Fee distribution configuration
    struct FeeDistribution {
        uint32 burnBps;               // % of base fee burned (basis points)
        uint32 treasuryBps;           // % of base fee to treasury
        address treasury;             // Treasury address
        // Note: priority fees always go to validators (not configurable)
    }

    /// @notice Complete chain fee configuration
    struct ChainFeeConfig {
        WeightCoefficients weights;
        BaseFeeParams baseFee;
        FeeDistribution distribution;
        uint32 maxTxBytes;            // Max tx size (0 = no limit)
        uint64 maxExecUnits;          // Max exec units (0 = no limit)
        uint32 maxStateTouches;       // Max state touches (0 = no limit)
        bool enabled;
        uint64 lastUpdated;
    }

    /// @notice D-Chain orderbook action fees
    struct OrderbookActionFees {
        uint64 placeOrderBaseUnits;   // Base fee-units for placing order
        uint64 cancelOrderBaseUnits;  // Base fee-units for cancel
        uint64 modifyOrderBaseUnits;  // Base fee-units for modify
        uint64 matchTradeBaseUnits;   // Base fee-units for match (can be 0)
        uint32 makerRebateBps;        // Maker rebate from priority fee
    }

    // ============================================================
    // Chain Constants
    // ============================================================

    uint8 public constant CHAIN_P = 0;
    uint8 public constant CHAIN_X = 1;
    uint8 public constant CHAIN_A = 2;
    uint8 public constant CHAIN_B = 3;
    uint8 public constant CHAIN_C = 4;
    uint8 public constant CHAIN_D = 5;
    uint8 public constant CHAIN_T = 6;
    uint8 public constant CHAIN_G = 7;
    uint8 public constant CHAIN_Q = 8;
    uint8 public constant CHAIN_K = 9;
    uint8 public constant CHAIN_Z = 10;
    uint8 public constant NUM_CHAINS = 11;

    // ============================================================
    // State
    // ============================================================

    mapping(uint8 => ChainFeeConfig) public chainConfigs;
    OrderbookActionFees public orderbookFees;
    mapping(bytes32 => ChainFeeConfig) public chainConfigs;
    uint256 public version;
    address public warpEmitter;

    // ============================================================
    // Events
    // ============================================================

    event BaseFeeUpdated(uint8 indexed chainId, uint64 oldBase, uint64 newBase);
    event ChainConfigUpdated(uint8 indexed chainId, uint256 version);
    event OrderbookFeesUpdated(uint256 version);
    event FeeDistributionUpdated(uint8 indexed chainId, uint32 burnBps, uint32 treasuryBps);
    event FeeBurned(uint8 indexed chainId, uint256 amount);
    event FeeSentToTreasury(uint8 indexed chainId, uint256 amount);
    event ConfigChangeProposed(bytes32 indexed proposalId, uint8 indexed chainId, uint256 executeAfter);
    event ConfigChangeExecuted(bytes32 indexed proposalId, uint8 indexed chainId);
    event ConfigChangeCancelled(bytes32 indexed proposalId);
    event DistributionChangeProposed(bytes32 indexed proposalId, uint8 indexed chainId, uint256 executeAfter);
    event DistributionChangeExecuted(bytes32 indexed proposalId, uint8 indexed chainId);
    event DistributionChangeCancelled(bytes32 indexed proposalId);

    // ============================================================
    // Constructor
    // ============================================================

    constructor(address _governor, address _feeUpdater, address _treasury) {
        _grantRole(DEFAULT_ADMIN_ROLE, msg.sender);
        _grantRole(GOVERNOR_ROLE, _governor);
        _grantRole(FEE_UPDATER_ROLE, _feeUpdater);
        _grantRole(EMERGENCY_ROLE, msg.sender);

        _initializeDefaults(_treasury);
    }

    function _initializeDefaults(address _treasury) internal {
        // Default fee distribution: 70% burn, 30% treasury
        FeeDistribution memory defaultDist = FeeDistribution({
            burnBps: 7000,
            treasuryBps: 3000,
            treasury: _treasury
        });

        // P-Chain: Infrastructure
        chainConfigs[CHAIN_P] = ChainFeeConfig({
            weights: WeightCoefficients({
                pByteMicroLux: 10,
                pExecMicroLux: 1,
                pStateMicroLux: 50
            }),
            baseFee: BaseFeeParams({
                basePerUnit: 1,              // 1 µLUX per fee-unit
                minBasePerUnit: 1,
                maxBasePerUnit: 1000,        // 1000x max
                targetUtilization: 5000,     // 50%
                maxChangePerBlock: 1250      // 12.5%
            }),
            distribution: defaultDist,
            maxTxBytes: 64000,
            maxExecUnits: 0,
            maxStateTouches: 0,
            enabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // X-Chain: UTXO exchange
        chainConfigs[CHAIN_X] = chainConfigs[CHAIN_P];
        chainConfigs[CHAIN_X].maxTxBytes = 128000;

        // A-Chain: Attestations
        chainConfigs[CHAIN_A] = ChainFeeConfig({
            weights: WeightCoefficients({
                pByteMicroLux: 15,
                pExecMicroLux: 5,
                pStateMicroLux: 30
            }),
            baseFee: BaseFeeParams({
                basePerUnit: 1,
                minBasePerUnit: 1,
                maxBasePerUnit: 2000,
                targetUtilization: 6000,     // 60%
                maxChangePerBlock: 1500      // 15%
            }),
            distribution: defaultDist,
            maxTxBytes: 32000,
            maxExecUnits: 1000,
            maxStateTouches: 100,
            enabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // B-Chain: Bridge (conservative)
        chainConfigs[CHAIN_B] = ChainFeeConfig({
            weights: WeightCoefficients({
                pByteMicroLux: 20,
                pExecMicroLux: 50,
                pStateMicroLux: 100
            }),
            baseFee: BaseFeeParams({
                basePerUnit: 2,
                minBasePerUnit: 1,
                maxBasePerUnit: 5000,
                targetUtilization: 4000,     // 40% (conservative)
                maxChangePerBlock: 2000      // 20%
            }),
            distribution: defaultDist,
            maxTxBytes: 16000,
            maxExecUnits: 10000,
            maxStateTouches: 50,
            enabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // C-Chain: EVM (gas-like)
        chainConfigs[CHAIN_C] = ChainFeeConfig({
            weights: WeightCoefficients({
                pByteMicroLux: 5,
                pExecMicroLux: 1,            // This IS gas
                pStateMicroLux: 100
            }),
            baseFee: BaseFeeParams({
                basePerUnit: 1,
                minBasePerUnit: 1,
                maxBasePerUnit: 1000,
                targetUtilization: 5000,
                maxChangePerBlock: 1250
            }),
            distribution: defaultDist,
            maxTxBytes: 0,
            maxExecUnits: 30000000,          // 30M gas limit
            maxStateTouches: 0,
            enabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // D-Chain: DEX
        chainConfigs[CHAIN_D] = ChainFeeConfig({
            weights: WeightCoefficients({
                pByteMicroLux: 10,
                pExecMicroLux: 2,
                pStateMicroLux: 20
            }),
            baseFee: BaseFeeParams({
                basePerUnit: 1,
                minBasePerUnit: 1,
                maxBasePerUnit: 2000,
                targetUtilization: 6000,
                maxChangePerBlock: 1500
            }),
            distribution: defaultDist,
            maxTxBytes: 4096,
            maxExecUnits: 5000,
            maxStateTouches: 50,
            enabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // D-Chain orderbook action fees
        orderbookFees = OrderbookActionFees({
            placeOrderBaseUnits: 500,
            cancelOrderBaseUnits: 350,
            modifyOrderBaseUnits: 400,
            matchTradeBaseUnits: 0,
            makerRebateBps: 50             // 0.5% rebate from priority
        });

        // T-Chain: Threshold/FHE (expensive)
        chainConfigs[CHAIN_T] = ChainFeeConfig({
            weights: WeightCoefficients({
                pByteMicroLux: 25,
                pExecMicroLux: 100,
                pStateMicroLux: 200
            }),
            baseFee: BaseFeeParams({
                basePerUnit: 3,
                minBasePerUnit: 1,
                maxBasePerUnit: 10000,
                targetUtilization: 4000,
                maxChangePerBlock: 2500
            }),
            distribution: defaultDist,
            maxTxBytes: 65536,
            maxExecUnits: 100000,
            maxStateTouches: 100,
            enabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // G-Chain: Graph (data-heavy)
        chainConfigs[CHAIN_G] = ChainFeeConfig({
            weights: WeightCoefficients({
                pByteMicroLux: 50,           // HIGH per-byte
                pExecMicroLux: 1,
                pStateMicroLux: 10
            }),
            baseFee: BaseFeeParams({
                basePerUnit: 1,
                minBasePerUnit: 1,
                maxBasePerUnit: 500,
                targetUtilization: 6000,
                maxChangePerBlock: 1000
            }),
            distribution: defaultDist,
            maxTxBytes: 1048576,             // 1MB
            maxExecUnits: 0,
            maxStateTouches: 0,
            enabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // Q-Chain: Quantum proofs
        chainConfigs[CHAIN_Q] = ChainFeeConfig({
            weights: WeightCoefficients({
                pByteMicroLux: 30,
                pExecMicroLux: 75,
                pStateMicroLux: 150
            }),
            baseFee: BaseFeeParams({
                basePerUnit: 2,
                minBasePerUnit: 1,
                maxBasePerUnit: 8000,
                targetUtilization: 4000,
                maxChangePerBlock: 2000
            }),
            distribution: defaultDist,
            maxTxBytes: 262144,
            maxExecUnits: 50000,
            maxStateTouches: 100,
            enabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // K-Chain: KMS
        chainConfigs[CHAIN_K] = ChainFeeConfig({
            weights: WeightCoefficients({
                pByteMicroLux: 20,
                pExecMicroLux: 30,
                pStateMicroLux: 80
            }),
            baseFee: BaseFeeParams({
                basePerUnit: 1,
                minBasePerUnit: 1,
                maxBasePerUnit: 2000,
                targetUtilization: 5000,
                maxChangePerBlock: 1500
            }),
            distribution: defaultDist,
            maxTxBytes: 32768,
            maxExecUnits: 10000,
            maxStateTouches: 50,
            enabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // Z-Chain: ZK (most expensive)
        chainConfigs[CHAIN_Z] = ChainFeeConfig({
            weights: WeightCoefficients({
                pByteMicroLux: 40,
                pExecMicroLux: 150,
                pStateMicroLux: 250
            }),
            baseFee: BaseFeeParams({
                basePerUnit: 3,
                minBasePerUnit: 1,
                maxBasePerUnit: 10000,
                targetUtilization: 4000,
                maxChangePerBlock: 2500
            }),
            distribution: defaultDist,
            maxTxBytes: 524288,
            maxExecUnits: 200000,
            maxStateTouches: 100,
            enabled: true,
            lastUpdated: uint64(block.timestamp)
        });
    }

    // ============================================================
    // Fee Calculation (EIP-1559 Style)
    // ============================================================

    /**
     * @notice Calculate fee-units for a transaction
     * @param chainId Chain ID
     * @param txBytes Transaction size
     * @param execUnits Execution units
     * @param stateTouches State touches
     * @return feeUnits Total fee-units (w(tx))
     */
    function calculateFeeUnits(
        uint8 chainId,
        uint32 txBytes,
        uint64 execUnits,
        uint32 stateTouches
    ) public view returns (uint64 feeUnits) {
        require(chainId < NUM_CHAINS, "Invalid chain");
        WeightCoefficients memory w = chainConfigs[chainId].weights;

        // w(tx) = pByte × bytes + pExec × exec + pState × state
        uint256 units = uint256(w.pByteMicroLux) * txBytes +
                        uint256(w.pExecMicroLux) * execUnits +
                        uint256(w.pStateMicroLux) * stateTouches;

        return uint64(units);
    }

    /**
     * @notice Check if a transaction is includable
     * @param chainId Chain ID
     * @param maxFeePerUnit User's max fee per unit (µLUX)
     * @return includable True if tx can be included
     */
    function isIncludable(
        uint8 chainId,
        uint64 maxFeePerUnit
    ) public view returns (bool includable) {
        require(chainId < NUM_CHAINS, "Invalid chain");
        return maxFeePerUnit >= chainConfigs[chainId].baseFee.basePerUnit;
    }

    /**
     * @notice Calculate effective tip per unit
     * @param chainId Chain ID
     * @param maxFeePerUnit User's max fee per unit
     * @param maxPriorityFeePerUnit User's max priority fee per unit
     * @return effectiveTip Actual tip per unit
     */
    function calculateEffectiveTip(
        uint8 chainId,
        uint64 maxFeePerUnit,
        uint64 maxPriorityFeePerUnit
    ) public view returns (uint64 effectiveTip) {
        require(chainId < NUM_CHAINS, "Invalid chain");
        uint64 basePerUnit = chainConfigs[chainId].baseFee.basePerUnit;

        // effectiveTip = min(maxPriorityFee, maxFee - baseFee)
        if (maxFeePerUnit < basePerUnit) {
            return 0; // Not includable
        }

        uint64 headroom = maxFeePerUnit - basePerUnit;
        return headroom < maxPriorityFeePerUnit ? headroom : maxPriorityFeePerUnit;
    }

    /**
     * @notice Calculate total fee for a transaction
     * @param chainId Chain ID
     * @param txBytes Transaction size
     * @param execUnits Execution units
     * @param stateTouches State touches
     * @param maxFeePerUnit User's max fee per unit
     * @param maxPriorityFeePerUnit User's max priority fee per unit
     * @return totalFee Total fee paid (µLUX)
     * @return baseFee Base fee portion (µLUX)
     * @return priorityFee Priority fee portion (µLUX)
     */
    function calculateTotalFee(
        uint8 chainId,
        uint32 txBytes,
        uint64 execUnits,
        uint32 stateTouches,
        uint64 maxFeePerUnit,
        uint64 maxPriorityFeePerUnit
    ) public view returns (
        uint64 totalFee,
        uint64 baseFee,
        uint64 priorityFee
    ) {
        require(chainId < NUM_CHAINS, "Invalid chain");

        uint64 feeUnits = calculateFeeUnits(chainId, txBytes, execUnits, stateTouches);
        uint64 basePerUnit = chainConfigs[chainId].baseFee.basePerUnit;
        uint64 effectiveTip = calculateEffectiveTip(chainId, maxFeePerUnit, maxPriorityFeePerUnit);

        // totalPaid = w(tx) × (basePerUnit + effectiveTipPerUnit)
        baseFee = uint64(uint256(feeUnits) * basePerUnit);
        priorityFee = uint64(uint256(feeUnits) * effectiveTip);
        totalFee = baseFee + priorityFee;
    }

    /**
     * @notice Calculate orderbook action fee
     * @param action 0=place, 1=cancel, 2=modify, 3=match
     * @param orderBytes Additional order data bytes
     * @param maxFeePerUnit User's max fee per unit
     * @param maxPriorityFeePerUnit User's max priority fee per unit
     */
    function calculateOrderbookFee(
        uint8 action,
        uint32 orderBytes,
        uint64 maxFeePerUnit,
        uint64 maxPriorityFeePerUnit
    ) public view returns (
        uint64 totalFee,
        uint64 baseFee,
        uint64 priorityFee
    ) {
        OrderbookActionFees memory af = orderbookFees;
        uint64 baseUnits;

        if (action == 0) {
            baseUnits = af.placeOrderBaseUnits;
        } else if (action == 1) {
            baseUnits = af.cancelOrderBaseUnits;
        } else if (action == 2) {
            baseUnits = af.modifyOrderBaseUnits;
        } else if (action == 3) {
            baseUnits = af.matchTradeBaseUnits;
        } else {
            revert("Invalid action");
        }

        // Add per-byte component for order data
        uint64 feeUnits = baseUnits + uint64(uint256(chainConfigs[CHAIN_D].weights.pByteMicroLux) * orderBytes);

        uint64 basePerUnit = chainConfigs[CHAIN_D].baseFee.basePerUnit;
        uint64 effectiveTip = calculateEffectiveTip(CHAIN_D, maxFeePerUnit, maxPriorityFeePerUnit);

        baseFee = uint64(uint256(feeUnits) * basePerUnit);
        priorityFee = uint64(uint256(feeUnits) * effectiveTip);
        totalFee = baseFee + priorityFee;
    }

    // ============================================================
    // Base Fee Update (EIP-1559 Algorithm)
    // ============================================================

    /**
     * @notice Update base fee after a block (called by validators)
     * @param chainId Chain ID
     * @param blockWeight Actual block weight used
     * @param maxBlockWeight Maximum block weight capacity
     */
    function updateBaseFee(
        uint8 chainId,
        uint64 blockWeight,
        uint64 maxBlockWeight
    ) external onlyRole(FEE_UPDATER_ROLE) {
        require(chainId < NUM_CHAINS, "Invalid chain");

        ChainFeeConfig storage config = chainConfigs[chainId];
        BaseFeeParams storage params = config.baseFee;

        // Target weight = maxWeight × targetUtilization
        uint64 targetWeight = uint64((uint256(maxBlockWeight) * params.targetUtilization) / 10000);

        uint64 oldBase = params.basePerUnit;
        uint64 newBase;

        if (blockWeight == targetWeight) {
            // Exactly at target, no change
            newBase = oldBase;
        } else if (blockWeight > targetWeight) {
            // Above target, increase base fee
            // delta = oldBase × maxChange × (used - target) / target
            uint256 delta = (uint256(oldBase) * params.maxChangePerBlock *
                            (blockWeight - targetWeight)) / (10000 * targetWeight);
            newBase = oldBase + uint64(delta);
            if (newBase > params.maxBasePerUnit) {
                newBase = params.maxBasePerUnit;
            }
        } else {
            // Below target, decrease base fee
            uint256 delta = (uint256(oldBase) * params.maxChangePerBlock *
                            (targetWeight - blockWeight)) / (10000 * targetWeight);
            if (delta >= oldBase) {
                newBase = params.minBasePerUnit;
            } else {
                newBase = oldBase - uint64(delta);
                if (newBase < params.minBasePerUnit) {
                    newBase = params.minBasePerUnit;
                }
            }
        }

        params.basePerUnit = newBase;
        config.lastUpdated = uint64(block.timestamp);

        emit BaseFeeUpdated(chainId, oldBase, newBase);
    }

    // ============================================================
    // Fee Distribution
    // ============================================================

    /**
     * @notice Distribute collected base fees (burn + treasury)
     * @param chainId Chain ID
     * @param baseFeeCollected Total base fees collected (µLUX)
     */
    function distributeBaseFees(
        uint8 chainId,
        uint256 baseFeeCollected
    ) external onlyRole(FEE_UPDATER_ROLE) {
        require(chainId < NUM_CHAINS, "Invalid chain");

        FeeDistribution memory dist = chainConfigs[chainId].distribution;

        uint256 toBurn = (baseFeeCollected * dist.burnBps) / 10000;
        uint256 toTreasury = baseFeeCollected - toBurn;

        if (toBurn > 0) {
            // In practice: transfer to burn address or call burn function
            emit FeeBurned(chainId, toBurn);
        }

        if (toTreasury > 0 && dist.treasury != address(0)) {
            // In practice: transfer to treasury
            emit FeeSentToTreasury(chainId, toTreasury);
        }
    }

    // ============================================================
    // Governance (With Timelock)
    // ============================================================

    /**
     * @notice Propose a chain config change (48 hour timelock)
     */
    function proposeChainConfig(uint8 chainId, ChainFeeConfig calldata config)
        external
        onlyRole(GOVERNOR_ROLE)
        returns (bytes32 proposalId)
    {
        require(chainId < NUM_CHAINS, "Invalid chain");
        require(config.distribution.burnBps + config.distribution.treasuryBps == 10000, "Must sum to 100%");

        proposalId = keccak256(abi.encode(chainId, config, block.timestamp));
        uint256 executeAfter = block.timestamp + TIMELOCK_DELAY;

        pendingConfigChanges[proposalId] = PendingConfigChange({
            chainId: chainId,
            config: config,
            executeAfter: executeAfter,
            exists: true
        });

        emit ConfigChangeProposed(proposalId, chainId, executeAfter);
    }

    /**
     * @notice Execute a pending chain config change after timelock
     */
    function executeChainConfig(bytes32 proposalId)
        external
        onlyRole(GOVERNOR_ROLE)
        nonReentrant
    {
        PendingConfigChange storage pending = pendingConfigChanges[proposalId];
        require(pending.exists, "Proposal does not exist");
        require(block.timestamp >= pending.executeAfter, "Timelock not expired");

        uint8 chainId = pending.chainId;
        chainConfigs[chainId] = pending.config;
        chainConfigs[chainId].lastUpdated = uint64(block.timestamp);
        version++;

        delete pendingConfigChanges[proposalId];

        emit ChainConfigUpdated(chainId, version);
        emit ConfigChangeExecuted(proposalId, chainId);
    }

    /**
     * @notice Cancel a pending config change
     */
    function cancelChainConfigProposal(bytes32 proposalId)
        external
        onlyRole(GOVERNOR_ROLE)
    {
        require(pendingConfigChanges[proposalId].exists, "Proposal does not exist");
        delete pendingConfigChanges[proposalId];
        emit ConfigChangeCancelled(proposalId);
    }

    /**
     * @notice Propose a fee distribution change (48 hour timelock)
     */
    function proposeFeeDistribution(uint8 chainId, FeeDistribution calldata dist)
        external
        onlyRole(GOVERNOR_ROLE)
        returns (bytes32 proposalId)
    {
        require(chainId < NUM_CHAINS, "Invalid chain");
        require(dist.burnBps + dist.treasuryBps == 10000, "Must sum to 100%");

        proposalId = keccak256(abi.encode(chainId, dist, block.timestamp));
        uint256 executeAfter = block.timestamp + TIMELOCK_DELAY;

        pendingDistributionChanges[proposalId] = PendingDistributionChange({
            chainId: chainId,
            distribution: dist,
            executeAfter: executeAfter,
            exists: true
        });

        emit DistributionChangeProposed(proposalId, chainId, executeAfter);
    }

    /**
     * @notice Execute a pending distribution change after timelock
     */
    function executeFeeDistribution(bytes32 proposalId)
        external
        onlyRole(GOVERNOR_ROLE)
        nonReentrant
    {
        PendingDistributionChange storage pending = pendingDistributionChanges[proposalId];
        require(pending.exists, "Proposal does not exist");
        require(block.timestamp >= pending.executeAfter, "Timelock not expired");

        uint8 chainId = pending.chainId;
        chainConfigs[chainId].distribution = pending.distribution;

        delete pendingDistributionChanges[proposalId];

        emit FeeDistributionUpdated(chainId, pending.distribution.burnBps, pending.distribution.treasuryBps);
        emit DistributionChangeExecuted(proposalId, chainId);
    }

    /**
     * @notice Cancel a pending distribution change
     */
    function cancelFeeDistributionProposal(bytes32 proposalId)
        external
        onlyRole(GOVERNOR_ROLE)
    {
        require(pendingDistributionChanges[proposalId].exists, "Proposal does not exist");
        delete pendingDistributionChanges[proposalId];
        emit DistributionChangeCancelled(proposalId);
    }

    /**
     * @notice Emergency override (skips timelock) - use only in critical situations
     */
    function emergencySetChainConfig(uint8 chainId, ChainFeeConfig calldata config)
        external
        onlyRole(EMERGENCY_ROLE)
        nonReentrant
    {
        require(chainId < NUM_CHAINS, "Invalid chain");
        require(config.distribution.burnBps + config.distribution.treasuryBps == 10000, "Must sum to 100%");

        chainConfigs[chainId] = config;
        chainConfigs[chainId].lastUpdated = uint64(block.timestamp);
        version++;

        emit ChainConfigUpdated(chainId, version);
    }

    function setOrderbookFees(OrderbookActionFees calldata fees)
        external
        onlyRole(GOVERNOR_ROLE)
    {
        orderbookFees = fees;
        version++;
        emit OrderbookFeesUpdated(version);
    }

    // ============================================================
    // View Functions
    // ============================================================

    function getChainConfig(uint8 chainId) external view returns (ChainFeeConfig memory) {
        require(chainId < NUM_CHAINS, "Invalid chain");
        return chainConfigs[chainId];
    }

    function getCurrentBasePerUnit(uint8 chainId) external view returns (uint64) {
        require(chainId < NUM_CHAINS, "Invalid chain");
        return chainConfigs[chainId].baseFee.basePerUnit;
    }

    function getAllBasePerUnit() external view returns (uint64[11] memory bases) {
        for (uint8 i = 0; i < NUM_CHAINS; i++) {
            bases[i] = chainConfigs[i].baseFee.basePerUnit;
        }
    }

    function getChainName(uint8 chainId) external pure returns (string memory) {
        if (chainId == CHAIN_P) return "P-Chain";
        if (chainId == CHAIN_X) return "X-Chain";
        if (chainId == CHAIN_A) return "A-Chain";
        if (chainId == CHAIN_B) return "B-Chain";
        if (chainId == CHAIN_C) return "C-Chain";
        if (chainId == CHAIN_D) return "D-Chain";
        if (chainId == CHAIN_T) return "T-Chain";
        if (chainId == CHAIN_G) return "G-Chain";
        if (chainId == CHAIN_Q) return "Q-Chain";
        if (chainId == CHAIN_K) return "K-Chain";
        if (chainId == CHAIN_Z) return "Z-Chain";
        revert("Invalid chain ID");
    }
}
