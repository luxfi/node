// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/governance/TimelockController.sol";

/**
 * @title FeeTimelock
 * @notice Timelock controller for fee governance proposals
 * @dev Enforces delay between proposal passing and execution
 *
 * Delays:
 *   - Normal fee updates: 24 hours (allows validators to prepare)
 *   - Emergency actions: 1 hour (quick response, still auditable)
 *
 * Roles:
 *   - PROPOSER_ROLE: FeeGovernor contract (after vote passes)
 *   - EXECUTOR_ROLE: Anyone can execute after delay
 *   - CANCELLER_ROLE: Emergency multisig
 */
contract FeeTimelock is TimelockController {

    /// @notice Minimum delay for normal operations (24 hours)
    uint256 public constant NORMAL_DELAY = 24 hours;

    /// @notice Minimum delay for emergency operations (1 hour)
    uint256 public constant EMERGENCY_DELAY = 1 hours;

    /// @notice Maximum delay allowed (7 days)
    uint256 public constant MAX_DELAY = 7 days;

    /**
     * @notice Initialize the timelock
     * @param minDelay Initial minimum delay (should be NORMAL_DELAY)
     * @param proposers Array of addresses that can propose (FeeGovernor)
     * @param executors Array of addresses that can execute (address(0) for anyone)
     * @param admin Admin address (can be renounced after setup)
     */
    constructor(
        uint256 minDelay,
        address[] memory proposers,
        address[] memory executors,
        address admin
    ) TimelockController(minDelay, proposers, executors, admin) {
        require(minDelay >= EMERGENCY_DELAY, "Delay too short");
        require(minDelay <= MAX_DELAY, "Delay too long");
    }

    /**
     * @notice Get the recommended delay for fee updates
     * @param chainId The chain being updated
     * @return delay Recommended delay in seconds
     */
    function getRecommendedDelay(uint8 chainId) external pure returns (uint256 delay) {
        // C-Chain (EVM) and P-Chain (staking) require longer delays
        if (chainId == 0 || chainId == 4) {
            return NORMAL_DELAY;
        }
        // Other chains can have shorter delays
        return NORMAL_DELAY / 2; // 12 hours
    }

    /**
     * @notice Check if an operation would affect critical chains
     * @param target Target contract address
     * @param data Calldata for the operation
     * @return isCritical True if operation affects P-Chain or C-Chain
     */
    function isCriticalOperation(
        address target,
        bytes calldata data
    ) external pure returns (bool isCritical) {
        // Check if this is a setChainFee call for critical chains
        if (data.length >= 36) {
            // First 4 bytes are function selector
            // For setChainFee(uint8, ChainFeeParams), chainId is first param
            bytes4 selector = bytes4(data[:4]);

            // setChainFee selector: keccak256("setChainFee(uint8,(uint64,uint64,uint64,uint32,bool,bool,uint64))")
            // We check the chainId parameter
            if (selector == bytes4(keccak256("setChainFee(uint8,(uint64,uint64,uint64,uint32,bool,bool,uint64))"))) {
                uint8 chainId = uint8(data[35]); // chainId at offset 35 (4 + 31)
                return chainId == 0 || chainId == 4; // P-Chain or C-Chain
            }
        }
        return false;
    }
}
