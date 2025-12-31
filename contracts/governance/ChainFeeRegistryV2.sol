// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/AccessControl.sol";
import "@openzeppelin/contracts/utils/ReentrancyGuard.sol";

/**
 * @title ChainFeeRegistryV2
 * @notice Enhanced fee registry with per-byte pricing, congestion multipliers, and action fees
 * @dev Implements LP-9010 Fee Pricing Specification
 *
 * Fee Formula (all chains):
 *   fee = max(floor, M * (pByte * bytes + pExec * exec + pState * state))
 *
 * Where:
 *   - floor: Anti-dust minimum payment (µLUX)
 *   - pByte: Per-byte fee for network/storage pressure (µLUX)
 *   - pExec: Per-execution-unit fee (gas, VM steps, prover cycles) (µLUX)
 *   - pState: Per-state-touch fee (reads/writes, storage delta) (µLUX)
 *   - M: Congestion multiplier (dynamic, per chain)
 *
 * Chain IDs:
 *   0 = P-Chain (Platform) - Staking, validators, subnets
 *   1 = X-Chain (Exchange) - UTXO asset transfers
 *   2 = A-Chain (Attestation) - Oracles, supply proofs
 *   3 = B-Chain (Bridge) - MPC cross-chain interop
 *   4 = C-Chain (Contract) - EVM smart contracts
 *   5 = D-Chain (DEX) - Orderbook, matching engine
 *   6 = T-Chain (Threshold) - FHE, MPC, threshold crypto
 *   7 = G-Chain (Graph) - Indexing, pay-per-query
 *   8 = Q-Chain (Quantum) - PQ proof root, omni-chain consensus
 *   9 = K-Chain (KMS) - Key management, ML-KEM encrypt/decrypt
 *  10 = Z-Chain (Zero) - Zero-knowledge proofs, private compute
 */
contract ChainFeeRegistryV2 is AccessControl, ReentrancyGuard {

    bytes32 public constant GOVERNOR_ROLE = keccak256("GOVERNOR_ROLE");
    bytes32 public constant EMERGENCY_ROLE = keccak256("EMERGENCY_ROLE");
    bytes32 public constant CONGESTION_UPDATER_ROLE = keccak256("CONGESTION_UPDATER_ROLE");

    /// @notice Enhanced fee parameters for a chain
    struct ChainFeeParams {
        // Base pricing (all in µLUX = microLUX)
        uint64 floorMicroLux;     // Anti-dust minimum fee
        uint64 pByteMicroLux;     // Per-byte fee (network/storage pressure)
        uint64 pExecMicroLux;     // Per-exec-unit fee (gas, steps, cycles)
        uint64 pStateMicroLux;    // Per-state-touch fee (reads/writes)

        // Congestion parameters (basis points: 10000 = 1.0)
        uint32 targetUtilization; // Target utilization (e.g., 6000 = 60%)
        uint32 alpha;             // How fast fees rise (e.g., 5000 = 50% rise rate)
        uint32 mCap;              // Maximum multiplier cap (e.g., 50000 = 5x)
        uint32 currentMultiplier; // Current congestion multiplier (10000 = 1x)

        // Safety guardrails
        uint32 maxTxBytes;        // Max transaction size (0 = no limit)
        uint64 maxExecUnits;      // Max execution units per tx (0 = no limit)

        // State
        bool enabled;             // Whether chain accepts transactions
        uint64 lastUpdated;       // Timestamp of last update
    }

    /// @notice D-Chain specific action fees (orderbook operations)
    struct OrderbookActionFees {
        uint64 placeOrderFloor;   // Floor for placing an order
        uint64 placeOrderPerByte; // Per-byte for order data
        uint64 cancelOrderFee;    // Fee for canceling (anti-spam)
        uint64 modifyOrderFee;    // Fee for modify/replace
        uint64 matchTradeFee;     // Fee per trade match (can be 0)
        uint64 makerRebateBps;    // Maker rebate in basis points (e.g., 50 = 0.5%)
    }

    /// @notice Chain name constants
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

    /// @notice Emergency change constraints
    uint32 public constant MAX_EMERGENCY_FLOOR_FACTOR = 20000;  // 2x max
    uint32 public constant MAX_EMERGENCY_PBYTE_FACTOR = 30000;  // 3x max
    uint32 public constant EMERGENCY_QUORUM_MULTIPLIER = 2;     // 2x normal quorum

    /// @notice Fee parameters for each chain
    mapping(uint8 => ChainFeeParams) public chainFees;

    /// @notice D-Chain orderbook action fees
    OrderbookActionFees public orderbookFees;

    /// @notice Subnet fee parameters (subnetID => params)
    mapping(bytes32 => ChainFeeParams) public subnetFees;

    /// @notice Version for tracking updates
    uint256 public version;

    /// @notice Warp emitter contract address
    address public warpEmitter;

    /// @notice EMA utilization per chain (basis points, 10000 = 100%)
    mapping(uint8 => uint32) public chainUtilization;

    // Events
    event ChainFeeUpdated(uint8 indexed chainId, ChainFeeParams params, uint256 version);
    event CongestionUpdated(uint8 indexed chainId, uint32 utilization, uint32 multiplier);
    event OrderbookFeesUpdated(OrderbookActionFees fees, uint256 version);
    event SubnetFeeUpdated(bytes32 indexed subnetId, ChainFeeParams params, uint256 version);
    event WarpEmitterUpdated(address indexed oldEmitter, address indexed newEmitter);
    event EmergencyPause(uint8 indexed chainId, address indexed by);
    event EmergencyUnpause(uint8 indexed chainId, address indexed by);
    event EmergencyFeeAdjust(uint8 indexed chainId, uint64 oldFloor, uint64 newFloor);

    constructor(address _governor, address _emergency, address _congestionUpdater) {
        _grantRole(DEFAULT_ADMIN_ROLE, msg.sender);
        _grantRole(GOVERNOR_ROLE, _governor);
        _grantRole(EMERGENCY_ROLE, _emergency);
        _grantRole(CONGESTION_UPDATER_ROLE, _congestionUpdater);

        _initializeDefaults();
    }

    /// @notice Initialize default fee parameters based on LP-9010 recommendations
    function _initializeDefaults() internal {
        // P-Chain: Infrastructure / base ledger
        chainFees[CHAIN_P] = ChainFeeParams({
            floorMicroLux: 1000,
            pByteMicroLux: 10,
            pExecMicroLux: 1,
            pStateMicroLux: 50,
            targetUtilization: 6000,  // 60%
            alpha: 5000,              // 50% rise rate
            mCap: 50000,              // 5x max
            currentMultiplier: 10000, // 1x
            maxTxBytes: 64000,
            maxExecUnits: 0,
            enabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // X-Chain: Infrastructure / UTXO exchange
        chainFees[CHAIN_X] = ChainFeeParams({
            floorMicroLux: 1000,
            pByteMicroLux: 10,
            pExecMicroLux: 1,
            pStateMicroLux: 50,
            targetUtilization: 6000,
            alpha: 5000,
            mCap: 50000,
            currentMultiplier: 10000,
            maxTxBytes: 128000,
            maxExecUnits: 0,
            enabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // A-Chain: Attestations (spam-attractive, bump floor)
        chainFees[CHAIN_A] = ChainFeeParams({
            floorMicroLux: 750,       // Bumped from 500
            pByteMicroLux: 15,        // Higher per-byte for attestation data
            pExecMicroLux: 5,         // Signature verification cost
            pStateMicroLux: 30,
            targetUtilization: 7000,  // 70%
            alpha: 6000,
            mCap: 80000,              // 8x max (spam resistance)
            currentMultiplier: 10000,
            maxTxBytes: 32000,
            maxExecUnits: 1000,
            enabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // B-Chain: Bridge (heavy crypto verification)
        chainFees[CHAIN_B] = ChainFeeParams({
            floorMicroLux: 2500,      // Higher for safety
            pByteMicroLux: 20,
            pExecMicroLux: 50,        // MPC verification expensive
            pStateMicroLux: 100,
            targetUtilization: 5000,  // 50% (conservative)
            alpha: 8000,              // Fast rise
            mCap: 100000,             // 10x max (DoS protection)
            currentMultiplier: 10000,
            maxTxBytes: 16000,
            maxExecUnits: 10000,
            enabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // C-Chain: EVM smart contracts (gas-heavy)
        chainFees[CHAIN_C] = ChainFeeParams({
            floorMicroLux: 1000,
            pByteMicroLux: 5,         // Small relative to gas
            pExecMicroLux: 1,         // Gas pricing dominant
            pStateMicroLux: 100,      // Storage surcharge
            targetUtilization: 6000,
            alpha: 5000,
            mCap: 50000,
            currentMultiplier: 10000,
            maxTxBytes: 0,            // No limit (gas handles it)
            maxExecUnits: 30000000,   // 30M gas limit
            enabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // D-Chain: DEX orderbook (action-based)
        chainFees[CHAIN_D] = ChainFeeParams({
            floorMicroLux: 750,       // Base floor
            pByteMicroLux: 10,
            pExecMicroLux: 2,
            pStateMicroLux: 20,
            targetUtilization: 7000,
            alpha: 6000,
            mCap: 80000,
            currentMultiplier: 10000,
            maxTxBytes: 4096,
            maxExecUnits: 5000,
            enabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // Initialize D-Chain orderbook action fees
        orderbookFees = OrderbookActionFees({
            placeOrderFloor: 500,
            placeOrderPerByte: 5,
            cancelOrderFee: 350,      // ~70% of place (anti-cancel-storm)
            modifyOrderFee: 400,      // ~80% of place
            matchTradeFee: 0,         // Free matching (incentivize liquidity)
            makerRebateBps: 50        // 0.5% maker rebate
        });

        // T-Chain: Threshold FHE/MPC (crypto-heavy)
        chainFees[CHAIN_T] = ChainFeeParams({
            floorMicroLux: 3000,      // High floor for expensive ops
            pByteMicroLux: 25,
            pExecMicroLux: 100,       // FHE operations expensive
            pStateMicroLux: 200,
            targetUtilization: 5000,
            alpha: 10000,             // Fast rise (DoS surface)
            mCap: 200000,             // 20x max
            currentMultiplier: 10000,
            maxTxBytes: 65536,
            maxExecUnits: 100000,
            enabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // G-Chain: Graph indexing (data-heavy, per-byte dominant)
        chainFees[CHAIN_G] = ChainFeeParams({
            floorMicroLux: 400,       // Bumped from 100
            pByteMicroLux: 50,        // HIGH per-byte (prevents blob spam)
            pExecMicroLux: 1,
            pStateMicroLux: 10,
            targetUtilization: 7000,
            alpha: 4000,
            mCap: 50000,
            currentMultiplier: 10000,
            maxTxBytes: 1048576,      // 1MB max for queries
            maxExecUnits: 0,
            enabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // Q-Chain: Quantum proofs (prover-heavy)
        chainFees[CHAIN_Q] = ChainFeeParams({
            floorMicroLux: 2000,      // Higher for proof work
            pByteMicroLux: 30,
            pExecMicroLux: 75,        // Proof verification
            pStateMicroLux: 150,
            targetUtilization: 5000,
            alpha: 8000,
            mCap: 150000,             // 15x max
            currentMultiplier: 10000,
            maxTxBytes: 262144,       // 256KB (proof sizes)
            maxExecUnits: 50000,
            enabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // K-Chain: KMS (crypto ops, ML-KEM)
        chainFees[CHAIN_K] = ChainFeeParams({
            floorMicroLux: 1250,
            pByteMicroLux: 20,
            pExecMicroLux: 30,        // ML-KEM operations
            pStateMicroLux: 80,
            targetUtilization: 6000,
            alpha: 6000,
            mCap: 80000,
            currentMultiplier: 10000,
            maxTxBytes: 32768,
            maxExecUnits: 10000,
            enabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // Z-Chain: Zero-knowledge (ZK proving/verifying)
        chainFees[CHAIN_Z] = ChainFeeParams({
            floorMicroLux: 3500,      // Highest floor (ZK compute)
            pByteMicroLux: 40,
            pExecMicroLux: 150,       // ZK proof verification expensive
            pStateMicroLux: 250,
            targetUtilization: 5000,
            alpha: 10000,
            mCap: 200000,             // 20x max
            currentMultiplier: 10000,
            maxTxBytes: 524288,       // 512KB (large proofs)
            maxExecUnits: 200000,
            enabled: true,
            lastUpdated: uint64(block.timestamp)
        });
    }

    // ============================================================
    // Fee Calculation
    // ============================================================

    /**
     * @notice Calculate fee for a transaction
     * @param chainId The chain ID
     * @param txBytes Transaction size in bytes
     * @param execUnits Execution units (gas, steps, cycles)
     * @param stateTouches State touches (reads + writes)
     * @return fee The calculated fee in µLUX
     */
    function calculateFee(
        uint8 chainId,
        uint32 txBytes,
        uint64 execUnits,
        uint32 stateTouches
    ) external view returns (uint64 fee) {
        require(chainId < NUM_CHAINS, "Invalid chain ID");
        ChainFeeParams memory params = chainFees[chainId];

        // fee = max(floor, M * (pByte * bytes + pExec * exec + pState * state))
        uint256 baseFee = uint256(params.pByteMicroLux) * txBytes +
                          uint256(params.pExecMicroLux) * execUnits +
                          uint256(params.pStateMicroLux) * stateTouches;

        // Apply congestion multiplier (basis points)
        uint256 adjustedFee = (baseFee * params.currentMultiplier) / 10000;

        // Enforce floor
        if (adjustedFee < params.floorMicroLux) {
            return params.floorMicroLux;
        }

        return uint64(adjustedFee);
    }

    /**
     * @notice Calculate D-Chain orderbook action fee
     * @param action Action type: 0=place, 1=cancel, 2=modify, 3=match
     * @param orderBytes Size of order data
     * @return fee The calculated fee in µLUX
     */
    function calculateOrderbookFee(
        uint8 action,
        uint32 orderBytes
    ) external view returns (uint64 fee) {
        ChainFeeParams memory params = chainFees[CHAIN_D];
        OrderbookActionFees memory af = orderbookFees;

        uint256 baseFee;
        if (action == 0) { // Place order
            baseFee = af.placeOrderFloor + (uint256(af.placeOrderPerByte) * orderBytes);
        } else if (action == 1) { // Cancel order
            baseFee = af.cancelOrderFee;
        } else if (action == 2) { // Modify order
            baseFee = af.modifyOrderFee;
        } else if (action == 3) { // Match trade
            baseFee = af.matchTradeFee;
        } else {
            revert("Invalid action");
        }

        // Apply congestion multiplier
        uint256 adjustedFee = (baseFee * params.currentMultiplier) / 10000;

        // Enforce chain floor
        if (adjustedFee < params.floorMicroLux) {
            return params.floorMicroLux;
        }

        return uint64(adjustedFee);
    }

    // ============================================================
    // Congestion Management
    // ============================================================

    /**
     * @notice Update chain utilization and recalculate multiplier
     * @dev Called by validators after each block
     * @param chainId The chain ID
     * @param blockWeight Weight of the current block
     * @param targetWeight Target block weight
     */
    function updateCongestion(
        uint8 chainId,
        uint64 blockWeight,
        uint64 targetWeight
    ) external onlyRole(CONGESTION_UPDATER_ROLE) {
        require(chainId < NUM_CHAINS, "Invalid chain ID");
        require(targetWeight > 0, "Target weight must be positive");

        ChainFeeParams storage params = chainFees[chainId];

        // Calculate current utilization (basis points)
        uint32 currentUtil = uint32((uint256(blockWeight) * 10000) / targetWeight);
        if (currentUtil > 10000) currentUtil = 10000; // Cap at 100%

        // EMA smoothing: newUtil = 0.9 * oldUtil + 0.1 * currentUtil
        uint32 oldUtil = chainUtilization[chainId];
        uint32 newUtil = uint32((uint256(oldUtil) * 9 + uint256(currentUtil)) / 10);
        chainUtilization[chainId] = newUtil;

        // Calculate new multiplier
        uint32 newMultiplier = _calculateMultiplier(
            newUtil,
            params.targetUtilization,
            params.alpha,
            params.mCap
        );

        params.currentMultiplier = newMultiplier;
        params.lastUpdated = uint64(block.timestamp);

        emit CongestionUpdated(chainId, newUtil, newMultiplier);
    }

    /**
     * @notice Calculate congestion multiplier
     * @dev M = 1 if u <= target, else M = min(mCap, 1 + alpha * (u-t)/(1-t))
     */
    function _calculateMultiplier(
        uint32 utilization,
        uint32 target,
        uint32 alpha,
        uint32 mCap
    ) internal pure returns (uint32) {
        // If utilization <= target, multiplier = 1x
        if (utilization <= target) {
            return 10000;
        }

        // M = 1 + alpha * (u - t) / (1 - t)
        // All in basis points (10000 = 1.0)
        uint256 excess = utilization - target;
        uint256 remaining = 10000 - target;

        if (remaining == 0) {
            return mCap;
        }

        uint256 multiplier = 10000 + (alpha * excess) / remaining;

        // Cap the multiplier
        if (multiplier > mCap) {
            return mCap;
        }

        return uint32(multiplier);
    }

    // ============================================================
    // Governance Functions
    // ============================================================

    /**
     * @notice Update fee parameters for a chain (governance only)
     * @param chainId The chain ID (0-10)
     * @param params New fee parameters
     */
    function setChainFee(uint8 chainId, ChainFeeParams calldata params)
        external
        onlyRole(GOVERNOR_ROLE)
        nonReentrant
    {
        require(chainId < NUM_CHAINS, "Invalid chain ID");
        _validateFeeParams(params);

        chainFees[chainId] = ChainFeeParams({
            floorMicroLux: params.floorMicroLux,
            pByteMicroLux: params.pByteMicroLux,
            pExecMicroLux: params.pExecMicroLux,
            pStateMicroLux: params.pStateMicroLux,
            targetUtilization: params.targetUtilization,
            alpha: params.alpha,
            mCap: params.mCap,
            currentMultiplier: chainFees[chainId].currentMultiplier, // Preserve current
            maxTxBytes: params.maxTxBytes,
            maxExecUnits: params.maxExecUnits,
            enabled: params.enabled,
            lastUpdated: uint64(block.timestamp)
        });

        version++;
        emit ChainFeeUpdated(chainId, chainFees[chainId], version);

        _emitWarpUpdate(chainId);
    }

    /**
     * @notice Update D-Chain orderbook action fees
     * @param fees New orderbook fees
     */
    function setOrderbookFees(OrderbookActionFees calldata fees)
        external
        onlyRole(GOVERNOR_ROLE)
        nonReentrant
    {
        require(fees.cancelOrderFee > 0, "Cancel fee must be positive");

        orderbookFees = fees;
        version++;
        emit OrderbookFeesUpdated(fees, version);
    }

    /**
     * @notice Validate fee parameters
     */
    function _validateFeeParams(ChainFeeParams calldata params) internal pure {
        require(params.floorMicroLux > 0, "Floor must be positive");
        require(params.targetUtilization > 0 && params.targetUtilization < 10000, "Invalid target");
        require(params.alpha > 0, "Alpha must be positive");
        require(params.mCap >= 10000, "mCap must be >= 1x");
    }

    // ============================================================
    // Emergency Functions
    // ============================================================

    /**
     * @notice Emergency pause a chain
     */
    function emergencyPause(uint8 chainId) external onlyRole(EMERGENCY_ROLE) {
        require(chainId < NUM_CHAINS, "Invalid chain ID");
        chainFees[chainId].enabled = false;
        chainFees[chainId].lastUpdated = uint64(block.timestamp);
        version++;
        emit EmergencyPause(chainId, msg.sender);
        _emitWarpUpdate(chainId);
    }

    /**
     * @notice Emergency unpause a chain
     */
    function emergencyUnpause(uint8 chainId) external onlyRole(EMERGENCY_ROLE) {
        require(chainId < NUM_CHAINS, "Invalid chain ID");
        chainFees[chainId].enabled = true;
        chainFees[chainId].lastUpdated = uint64(block.timestamp);
        version++;
        emit EmergencyUnpause(chainId, msg.sender);
        _emitWarpUpdate(chainId);
    }

    /**
     * @notice Emergency floor adjustment (constrained to 2x max)
     * @param chainId The chain to adjust
     * @param newFloor New floor value (must be <= 2x current)
     */
    function emergencyAdjustFloor(uint8 chainId, uint64 newFloor)
        external
        onlyRole(EMERGENCY_ROLE)
    {
        require(chainId < NUM_CHAINS, "Invalid chain ID");
        uint64 currentFloor = chainFees[chainId].floorMicroLux;
        uint64 maxAllowed = uint64((uint256(currentFloor) * MAX_EMERGENCY_FLOOR_FACTOR) / 10000);

        require(newFloor <= maxAllowed, "Exceeds emergency limit (2x)");

        emit EmergencyFeeAdjust(chainId, currentFloor, newFloor);
        chainFees[chainId].floorMicroLux = newFloor;
        chainFees[chainId].lastUpdated = uint64(block.timestamp);
        version++;
        _emitWarpUpdate(chainId);
    }

    // ============================================================
    // View Functions
    // ============================================================

    /**
     * @notice Get all chain fees
     */
    function getAllChainFees() external view returns (ChainFeeParams[11] memory) {
        ChainFeeParams[11] memory fees;
        for (uint8 i = 0; i < NUM_CHAINS; i++) {
            fees[i] = chainFees[i];
        }
        return fees;
    }

    /**
     * @notice Get fee parameters for a specific chain
     */
    function getChainFee(uint8 chainId) external view returns (ChainFeeParams memory) {
        require(chainId < NUM_CHAINS, "Invalid chain ID");
        return chainFees[chainId];
    }

    /**
     * @notice Check if a chain is enabled
     */
    function isChainEnabled(uint8 chainId) external view returns (bool) {
        require(chainId < NUM_CHAINS, "Invalid chain ID");
        return chainFees[chainId].enabled;
    }

    /**
     * @notice Get current congestion multiplier for a chain
     */
    function getCongestionMultiplier(uint8 chainId) external view returns (uint32) {
        require(chainId < NUM_CHAINS, "Invalid chain ID");
        return chainFees[chainId].currentMultiplier;
    }

    /**
     * @notice Get chain name
     */
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

    // ============================================================
    // Warp Integration
    // ============================================================

    /**
     * @notice Set the Warp emitter contract
     */
    function setWarpEmitter(address _warpEmitter) external onlyRole(DEFAULT_ADMIN_ROLE) {
        address old = warpEmitter;
        warpEmitter = _warpEmitter;
        emit WarpEmitterUpdated(old, _warpEmitter);
    }

    /**
     * @notice Emit Warp update for a chain
     */
    function _emitWarpUpdate(uint8 chainId) internal {
        if (warpEmitter != address(0)) {
            IWarpFeeEmitterV2(warpEmitter).emitFeeUpdate(chainId, chainFees[chainId], version);
        }
    }
}

/// @notice Interface for WarpFeeEmitter V2
interface IWarpFeeEmitterV2 {
    function emitFeeUpdate(
        uint8 chainId,
        ChainFeeRegistryV2.ChainFeeParams calldata params,
        uint256 version
    ) external;
}
