// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/AccessControl.sol";
import "./ChainFeeRegistry.sol";

/**
 * @title WarpFeeEmitter
 * @notice Emits Warp messages for cross-chain fee updates
 * @dev Called by ChainFeeRegistry when fee parameters change
 *
 * Warp Message Flow:
 * 1. Governance updates ChainFeeRegistry
 * 2. ChainFeeRegistry calls WarpFeeEmitter.emitFeeUpdate()
 * 3. WarpFeeEmitter emits WarpFeeUpdate event
 * 4. Validators read event, create Warp message
 * 5. All chains receive fee update via Warp
 */
contract WarpFeeEmitter is AccessControl {

    bytes32 public constant REGISTRY_ROLE = keccak256("REGISTRY_ROLE");

    /// @notice Reference to the fee registry
    ChainFeeRegistry public immutable registry;

    /// @notice Warp message type for fee updates
    uint8 public constant WARP_TYPE_FEE_UPDATE = 0x01;
    uint8 public constant WARP_TYPE_BATCH_UPDATE = 0x02;
    uint8 public constant WARP_TYPE_EMERGENCY = 0x03;

    /// @notice Warp fee update payload
    struct FeeUpdatePayload {
        uint8 messageType;       // WARP_TYPE_*
        uint8 chainId;           // Target chain (0-10)
        uint64 baseFee;          // New base fee
        uint64 minFee;           // New min fee
        uint64 maxFee;           // New max fee
        uint32 congestionMult;   // Congestion multiplier
        bool enabled;            // Chain enabled
        bool feesEnabled;        // Fees required
        uint256 version;         // Registry version
        uint64 timestamp;        // Block timestamp
    }

    // Events - these are picked up by Warp validators
    event WarpFeeUpdate(
        uint8 indexed chainId,
        uint64 baseFee,
        uint64 minFee,
        uint64 maxFee,
        uint32 congestionMultiplier,
        bool enabled,
        bool feesEnabled,
        uint256 version,
        bytes payload
    );

    event WarpBatchFeeUpdate(
        uint8[] chainIds,
        uint256 version,
        bytes payload
    );

    event WarpEmergencyUpdate(
        uint8 indexed chainId,
        bool enabled,
        uint256 version,
        bytes payload
    );

    constructor(address _registry) {
        registry = ChainFeeRegistry(_registry);
        _grantRole(DEFAULT_ADMIN_ROLE, msg.sender);
        _grantRole(REGISTRY_ROLE, _registry);
    }

    /// @notice Emit a fee update Warp message
    /// @param chainId The chain being updated
    /// @param params New fee parameters
    /// @param version Registry version
    function emitFeeUpdate(
        uint8 chainId,
        ChainFeeRegistry.ChainFeeParams calldata params,
        uint256 version
    ) external onlyRole(REGISTRY_ROLE) {
        // Encode payload for Warp message
        bytes memory payload = abi.encode(
            FeeUpdatePayload({
                messageType: WARP_TYPE_FEE_UPDATE,
                chainId: chainId,
                baseFee: params.baseFee,
                minFee: params.minFee,
                maxFee: params.maxFee,
                congestionMult: params.congestionMultiplier,
                enabled: params.enabled,
                feesEnabled: params.feesEnabled,
                version: version,
                timestamp: uint64(block.timestamp)
            })
        );

        emit WarpFeeUpdate(
            chainId,
            params.baseFee,
            params.minFee,
            params.maxFee,
            params.congestionMultiplier,
            params.enabled,
            params.feesEnabled,
            version,
            payload
        );
    }

    /// @notice Emit a batch fee update Warp message
    /// @param chainIds Chains being updated
    /// @param version Registry version
    function emitBatchFeeUpdate(
        uint8[] calldata chainIds,
        uint256 version
    ) external onlyRole(REGISTRY_ROLE) {
        // Encode all chain fees into payload
        ChainFeeRegistry.ChainFeeParams[11] memory allFees = registry.getAllChainFees();

        bytes memory payload = abi.encode(
            WARP_TYPE_BATCH_UPDATE,
            chainIds,
            allFees,
            version,
            block.timestamp
        );

        emit WarpBatchFeeUpdate(chainIds, version, payload);
    }

    /// @notice Decode a fee update payload
    /// @param payload Encoded payload bytes
    /// @return decoded The decoded FeeUpdatePayload
    function decodePayload(bytes calldata payload)
        external
        pure
        returns (FeeUpdatePayload memory decoded)
    {
        decoded = abi.decode(payload, (FeeUpdatePayload));
    }

    /// @notice Get chain names for display
    function getChainNames() external pure returns (string[11] memory) {
        return [
            "P-Chain",  // 0 - Platform
            "X-Chain",  // 1 - Exchange
            "A-Chain",  // 2 - Attestation
            "B-Chain",  // 3 - Bridge
            "C-Chain",  // 4 - Contract
            "D-Chain",  // 5 - DEX
            "T-Chain",  // 6 - Threshold
            "G-Chain",  // 7 - Graph
            "Q-Chain",  // 8 - Quantum
            "K-Chain",  // 9 - KMS
            "Z-Chain"   // 10 - Zero
        ];
    }
}
