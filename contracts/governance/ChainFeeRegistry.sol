// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/AccessControl.sol";
import "@openzeppelin/contracts/utils/ReentrancyGuard.sol";

/**
 * @title ChainFeeRegistry
 * @notice Central registry for fee parameters across all Lux chains
 * @dev Controlled by FeeGovernor via FeeTimelock
 *
 * Chain IDs:
 *   0 = P-Chain (Platform) - Staking, validators, chains
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
contract ChainFeeRegistry is AccessControl, ReentrancyGuard {

    bytes32 public constant GOVERNOR_ROLE = keccak256("GOVERNOR_ROLE");
    bytes32 public constant EMERGENCY_ROLE = keccak256("EMERGENCY_ROLE");

    /// @notice Fee parameters for a chain
    struct ChainFeeParams {
        uint64 baseFee;           // Base fee in microLUX (1 LUX = 1_000_000 microLUX)
        uint64 minFee;            // Minimum fee cap
        uint64 maxFee;            // Maximum fee cap (0 = no cap)
        uint32 congestionMultiplier; // Multiplier for congestion (basis points, 10000 = 1x)
        bool enabled;             // Whether chain accepts transactions
        bool feesEnabled;         // Whether fees are required (false = free)
        uint64 lastUpdated;       // Timestamp of last update
    }

    /// @notice Chain name constants
    uint8 public constant CHAIN_P = 0;  // Platform
    uint8 public constant CHAIN_X = 1;  // Exchange
    uint8 public constant CHAIN_A = 2;  // Attestation
    uint8 public constant CHAIN_B = 3;  // Bridge
    uint8 public constant CHAIN_C = 4;  // Contract (EVM)
    uint8 public constant CHAIN_D = 5;  // DEX
    uint8 public constant CHAIN_T = 6;  // Threshold (FHE)
    uint8 public constant CHAIN_G = 7;  // Graph
    uint8 public constant CHAIN_Q = 8;  // Quantum
    uint8 public constant CHAIN_K = 9;  // KMS (ML-KEM)
    uint8 public constant CHAIN_Z = 10; // Zero (ZK)
    uint8 public constant NUM_CHAINS = 11;

    /// @notice Fee parameters for each chain
    mapping(uint8 => ChainFeeParams) public chainFees;

    /// @notice Chain fee parameters (chainID => params)
    mapping(bytes32 => ChainFeeParams) public chainFees;

    /// @notice Version for tracking updates (incremented on each change)
    uint256 public version;

    /// @notice Warp emitter contract address
    address public warpEmitter;

    // Events
    event ChainFeeUpdated(uint8 indexed chainId, ChainFeeParams params, uint256 version);
    event ChainFeeUpdated(bytes32 indexed chainId, ChainFeeParams params, uint256 version);
    event WarpEmitterUpdated(address indexed oldEmitter, address indexed newEmitter);
    event EmergencyPause(uint8 indexed chainId, address indexed by);
    event EmergencyUnpause(uint8 indexed chainId, address indexed by);

    constructor(address _governor, address _emergency) {
        _grantRole(DEFAULT_ADMIN_ROLE, msg.sender);
        _grantRole(GOVERNOR_ROLE, _governor);
        _grantRole(EMERGENCY_ROLE, _emergency);

        // Initialize default fee parameters for all chains
        _initializeDefaults();
    }

    /// @notice Initialize default fee parameters
    function _initializeDefaults() internal {
        // P-Chain: Staking operations (higher fees)
        chainFees[CHAIN_P] = ChainFeeParams({
            baseFee: 1000,              // 1000 microLUX
            minFee: 100,
            maxFee: 1_000_000,          // 1 LUX max
            congestionMultiplier: 10000, // 1x
            enabled: true,
            feesEnabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // X-Chain: UTXO transfers
        chainFees[CHAIN_X] = ChainFeeParams({
            baseFee: 1000,
            minFee: 100,
            maxFee: 100_000,
            congestionMultiplier: 10000,
            enabled: true,
            feesEnabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // A-Chain: Attestations (lower fees to encourage)
        chainFees[CHAIN_A] = ChainFeeParams({
            baseFee: 500,
            minFee: 50,
            maxFee: 50_000,
            congestionMultiplier: 10000,
            enabled: true,
            feesEnabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // B-Chain: Bridge (higher fees for security)
        chainFees[CHAIN_B] = ChainFeeParams({
            baseFee: 2000,
            minFee: 500,
            maxFee: 500_000,
            congestionMultiplier: 10000,
            enabled: true,
            feesEnabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // C-Chain: EVM (gas-based, base fee for priority)
        chainFees[CHAIN_C] = ChainFeeParams({
            baseFee: 1000,
            minFee: 100,
            maxFee: 0,                  // No cap (EVM gas handles it)
            congestionMultiplier: 10000,
            enabled: true,
            feesEnabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // D-Chain: DEX (competitive fees)
        chainFees[CHAIN_D] = ChainFeeParams({
            baseFee: 500,
            minFee: 100,
            maxFee: 100_000,
            congestionMultiplier: 10000,
            enabled: true,
            feesEnabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // T-Chain: Threshold (compute-intensive)
        chainFees[CHAIN_T] = ChainFeeParams({
            baseFee: 2000,
            minFee: 500,
            maxFee: 1_000_000,
            congestionMultiplier: 10000,
            enabled: true,
            feesEnabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // G-Chain: Graph (pay-per-query, lower base)
        chainFees[CHAIN_G] = ChainFeeParams({
            baseFee: 100,
            minFee: 10,
            maxFee: 10_000,
            congestionMultiplier: 10000,
            enabled: true,
            feesEnabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // Q-Chain: Quantum (proof submissions)
        chainFees[CHAIN_Q] = ChainFeeParams({
            baseFee: 1500,
            minFee: 500,
            maxFee: 500_000,
            congestionMultiplier: 10000,
            enabled: true,
            feesEnabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // K-Chain: KMS (ML-KEM encrypt/decrypt)
        chainFees[CHAIN_K] = ChainFeeParams({
            baseFee: 1000,
            minFee: 200,
            maxFee: 200_000,
            congestionMultiplier: 10000,
            enabled: true,
            feesEnabled: true,
            lastUpdated: uint64(block.timestamp)
        });

        // Z-Chain: Zero-Knowledge (ZK proofs, private compute)
        chainFees[CHAIN_Z] = ChainFeeParams({
            baseFee: 3000,              // Higher fees for ZK compute
            minFee: 1000,
            maxFee: 1_000_000,
            congestionMultiplier: 10000,
            enabled: true,
            feesEnabled: true,
            lastUpdated: uint64(block.timestamp)
        });
    }

    /// @notice Update fee parameters for a chain (governance only)
    /// @param chainId The chain ID (0-8)
    /// @param params New fee parameters
    function setChainFee(uint8 chainId, ChainFeeParams calldata params)
        external
        onlyRole(GOVERNOR_ROLE)
        nonReentrant
    {
        require(chainId < NUM_CHAINS, "Invalid chain ID");
        require(params.baseFee >= params.minFee, "Base fee below minimum");
        require(params.maxFee == 0 || params.baseFee <= params.maxFee, "Base fee above maximum");

        chainFees[chainId] = ChainFeeParams({
            baseFee: params.baseFee,
            minFee: params.minFee,
            maxFee: params.maxFee,
            congestionMultiplier: params.congestionMultiplier,
            enabled: params.enabled,
            feesEnabled: params.feesEnabled,
            lastUpdated: uint64(block.timestamp)
        });

        version++;
        emit ChainFeeUpdated(chainId, chainFees[chainId], version);

        // Emit Warp message if emitter is set
        if (warpEmitter != address(0)) {
            IWarpFeeEmitter(warpEmitter).emitFeeUpdate(chainId, chainFees[chainId], version);
        }
    }

    /// @notice Batch update multiple chain fees
    /// @param chainIds Array of chain IDs
    /// @param params Array of fee parameters
    function setChainFeesBatch(uint8[] calldata chainIds, ChainFeeParams[] calldata params)
        external
        onlyRole(GOVERNOR_ROLE)
        nonReentrant
    {
        require(chainIds.length == params.length, "Length mismatch");
        require(chainIds.length <= NUM_CHAINS, "Too many chains");

        for (uint256 i = 0; i < chainIds.length; i++) {
            require(chainIds[i] < NUM_CHAINS, "Invalid chain ID");
            require(params[i].baseFee >= params[i].minFee, "Base fee below minimum");

            chainFees[chainIds[i]] = ChainFeeParams({
                baseFee: params[i].baseFee,
                minFee: params[i].minFee,
                maxFee: params[i].maxFee,
                congestionMultiplier: params[i].congestionMultiplier,
                enabled: params[i].enabled,
                feesEnabled: params[i].feesEnabled,
                lastUpdated: uint64(block.timestamp)
            });

            emit ChainFeeUpdated(chainIds[i], chainFees[chainIds[i]], version + 1);
        }

        version++;

        // Emit batch Warp message
        if (warpEmitter != address(0)) {
            IWarpFeeEmitter(warpEmitter).emitBatchFeeUpdate(chainIds, version);
        }
    }

    /// @notice Update fee parameters for a chain
    /// @param chainId The chain ID (32 bytes)
    /// @param params New fee parameters
    function setChainFee(bytes32 chainId, ChainFeeParams calldata params)
        external
        onlyRole(GOVERNOR_ROLE)
        nonReentrant
    {
        require(params.baseFee >= params.minFee, "Base fee below minimum");

        chainFees[chainId] = ChainFeeParams({
            baseFee: params.baseFee,
            minFee: params.minFee,
            maxFee: params.maxFee,
            congestionMultiplier: params.congestionMultiplier,
            enabled: params.enabled,
            feesEnabled: params.feesEnabled,
            lastUpdated: uint64(block.timestamp)
        });

        version++;
        emit ChainFeeUpdated(chainId, chainFees[chainId], version);
    }

    /// @notice Emergency pause a chain (stops all transactions)
    /// @param chainId The chain to pause
    function emergencyPause(uint8 chainId) external onlyRole(EMERGENCY_ROLE) {
        require(chainId < NUM_CHAINS, "Invalid chain ID");
        chainFees[chainId].enabled = false;
        chainFees[chainId].lastUpdated = uint64(block.timestamp);
        version++;
        emit EmergencyPause(chainId, msg.sender);

        if (warpEmitter != address(0)) {
            IWarpFeeEmitter(warpEmitter).emitFeeUpdate(chainId, chainFees[chainId], version);
        }
    }

    /// @notice Emergency unpause a chain
    /// @param chainId The chain to unpause
    function emergencyUnpause(uint8 chainId) external onlyRole(EMERGENCY_ROLE) {
        require(chainId < NUM_CHAINS, "Invalid chain ID");
        chainFees[chainId].enabled = true;
        chainFees[chainId].lastUpdated = uint64(block.timestamp);
        version++;
        emit EmergencyUnpause(chainId, msg.sender);

        if (warpEmitter != address(0)) {
            IWarpFeeEmitter(warpEmitter).emitFeeUpdate(chainId, chainFees[chainId], version);
        }
    }

    /// @notice Set the Warp emitter contract
    /// @param _warpEmitter Address of WarpFeeEmitter contract
    function setWarpEmitter(address _warpEmitter) external onlyRole(DEFAULT_ADMIN_ROLE) {
        address old = warpEmitter;
        warpEmitter = _warpEmitter;
        emit WarpEmitterUpdated(old, _warpEmitter);
    }

    // ============================================================
    // View Functions
    // ============================================================

    /// @notice Get fee parameters for a chain
    function getChainFee(uint8 chainId) external view returns (ChainFeeParams memory) {
        require(chainId < NUM_CHAINS, "Invalid chain ID");
        return chainFees[chainId];
    }

    /// @notice Get effective fee for a chain (with congestion multiplier)
    function getEffectiveFee(uint8 chainId) external view returns (uint64) {
        require(chainId < NUM_CHAINS, "Invalid chain ID");
        ChainFeeParams memory params = chainFees[chainId];

        if (!params.feesEnabled) return 0;

        uint256 fee = (uint256(params.baseFee) * params.congestionMultiplier) / 10000;

        if (fee < params.minFee) return params.minFee;
        if (params.maxFee > 0 && fee > params.maxFee) return params.maxFee;

        return uint64(fee);
    }

    /// @notice Get all chain fees in one call
    function getAllChainFees() external view returns (ChainFeeParams[11] memory) {
        ChainFeeParams[11] memory fees;
        for (uint8 i = 0; i < NUM_CHAINS; i++) {
            fees[i] = chainFees[i];
        }
        return fees;
    }

    /// @notice Check if a chain is enabled
    function isChainEnabled(uint8 chainId) external view returns (bool) {
        require(chainId < NUM_CHAINS, "Invalid chain ID");
        return chainFees[chainId].enabled;
    }

    /// @notice Get chain name
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

/// @notice Interface for WarpFeeEmitter
interface IWarpFeeEmitter {
    function emitFeeUpdate(uint8 chainId, ChainFeeRegistry.ChainFeeParams calldata params, uint256 version) external;
    function emitBatchFeeUpdate(uint8[] calldata chainIds, uint256 version) external;
}
