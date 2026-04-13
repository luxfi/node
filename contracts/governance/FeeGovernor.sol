// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/governance/Governor.sol";
import "@openzeppelin/contracts/governance/extensions/GovernorSettings.sol";
import "@openzeppelin/contracts/governance/extensions/GovernorCountingSimple.sol";
import "@openzeppelin/contracts/governance/extensions/GovernorVotes.sol";
import "@openzeppelin/contracts/governance/extensions/GovernorVotesQuorumFraction.sol";
import "@openzeppelin/contracts/governance/extensions/GovernorTimelockControl.sol";
import "./ChainFeeRegistry.sol";

/**
 * @title FeeGovernor
 * @notice DAO governance for cross-chain fee management
 * @dev Controls ChainFeeRegistry through FeeTimelock
 *
 * Governance Parameters:
 *   - Voting Delay: 1 day (block-based)
 *   - Voting Period: 1 week
 *   - Proposal Threshold: 100,000 LUX (0.1% of supply)
 *   - Quorum: 4% of total voting power
 *
 * Supported Proposal Types:
 *   1. setChainFee(chainId, params) - Update single chain fees
 *   2. setChainFeesBatch(chainIds[], params[]) - Batch fee updates
 *   3. emergencyPause(chainId) - Emergency pause via multisig
 *   4. setChainFee(chainId, params) - Chain fee updates
 */
contract FeeGovernor is
    Governor,
    GovernorSettings,
    GovernorCountingSimple,
    GovernorVotes,
    GovernorVotesQuorumFraction,
    GovernorTimelockControl
{
    /// @notice Reference to the fee registry
    ChainFeeRegistry public immutable feeRegistry;

    /// @notice Minimum voting power required to create proposal
    uint256 public constant PROPOSAL_THRESHOLD = 100_000 * 1e18; // 100K LUX

    /// @notice Events for fee governance actions
    event FeeProposalCreated(
        uint256 indexed proposalId,
        uint8[] chainIds,
        address proposer,
        string description
    );

    event FeeProposalExecuted(
        uint256 indexed proposalId,
        uint8[] chainIds
    );

    /**
     * @notice Initialize the fee governor
     * @param _token The governance token (LUX staking token)
     * @param _timelock The timelock controller
     * @param _feeRegistry The fee registry contract
     */
    constructor(
        IVotes _token,
        TimelockController _timelock,
        ChainFeeRegistry _feeRegistry
    )
        Governor("Lux Fee Governor")
        GovernorSettings(
            7200,      // 1 day voting delay (~7200 blocks at 12s)
            50400,     // 1 week voting period (~50400 blocks at 12s)
            PROPOSAL_THRESHOLD
        )
        GovernorVotes(_token)
        GovernorVotesQuorumFraction(4) // 4% quorum
        GovernorTimelockControl(_timelock)
    {
        feeRegistry = _feeRegistry;
    }

    // ============================================================
    // Proposal Helpers
    // ============================================================

    /**
     * @notice Create a proposal to update a single chain's fees
     * @param chainId The chain to update (0-10)
     * @param params New fee parameters
     * @param description Human-readable description
     * @return proposalId The created proposal ID
     */
    function proposeFeeUpdate(
        uint8 chainId,
        ChainFeeRegistry.ChainFeeParams calldata params,
        string calldata description
    ) external returns (uint256 proposalId) {
        require(chainId < feeRegistry.NUM_CHAINS(), "Invalid chain ID");

        address[] memory targets = new address[](1);
        uint256[] memory values = new uint256[](1);
        bytes[] memory calldatas = new bytes[](1);

        targets[0] = address(feeRegistry);
        values[0] = 0;
        calldatas[0] = abi.encodeWithSelector(
            ChainFeeRegistry.setChainFee.selector,
            chainId,
            params
        );

        proposalId = propose(targets, values, calldatas, description);

        uint8[] memory chainIds = new uint8[](1);
        chainIds[0] = chainId;
        emit FeeProposalCreated(proposalId, chainIds, msg.sender, description);
    }

    /**
     * @notice Create a proposal to update multiple chains' fees
     * @param chainIds Array of chain IDs to update
     * @param params Array of fee parameters
     * @param description Human-readable description
     * @return proposalId The created proposal ID
     */
    function proposeBatchFeeUpdate(
        uint8[] calldata chainIds,
        ChainFeeRegistry.ChainFeeParams[] calldata params,
        string calldata description
    ) external returns (uint256 proposalId) {
        require(chainIds.length == params.length, "Length mismatch");
        require(chainIds.length <= feeRegistry.NUM_CHAINS(), "Too many chains");

        address[] memory targets = new address[](1);
        uint256[] memory values = new uint256[](1);
        bytes[] memory calldatas = new bytes[](1);

        targets[0] = address(feeRegistry);
        values[0] = 0;
        calldatas[0] = abi.encodeWithSelector(
            ChainFeeRegistry.setChainFeesBatch.selector,
            chainIds,
            params
        );

        proposalId = propose(targets, values, calldatas, description);
        emit FeeProposalCreated(proposalId, chainIds, msg.sender, description);
    }

    /**
     * @notice Create a proposal to update chain fees
     * @param chainId The chain ID (32 bytes)
     * @param params New fee parameters
     * @param description Human-readable description
     * @return proposalId The created proposal ID
     */
    function proposeChainFeeUpdate(
        bytes32 chainId,
        ChainFeeRegistry.ChainFeeParams calldata params,
        string calldata description
    ) external returns (uint256 proposalId) {
        address[] memory targets = new address[](1);
        uint256[] memory values = new uint256[](1);
        bytes[] memory calldatas = new bytes[](1);

        targets[0] = address(feeRegistry);
        values[0] = 0;
        calldatas[0] = abi.encodeWithSelector(
            ChainFeeRegistry.setChainFee.selector,
            chainId,
            params
        );

        proposalId = propose(targets, values, calldatas, description);

        // Emit with empty chainIds (chain update)
        uint8[] memory chainIds = new uint8[](0);
        emit FeeProposalCreated(proposalId, chainIds, msg.sender, description);
    }

    // ============================================================
    // View Functions
    // ============================================================

    /**
     * @notice Get current fee parameters for all chains
     * @return All chain fee parameters
     */
    function getAllChainFees() external view returns (ChainFeeRegistry.ChainFeeParams[11] memory) {
        return feeRegistry.getAllChainFees();
    }

    /**
     * @notice Get fee parameters for a specific chain
     * @param chainId The chain ID
     * @return Fee parameters
     */
    function getChainFee(uint8 chainId) external view returns (ChainFeeRegistry.ChainFeeParams memory) {
        return feeRegistry.getChainFee(chainId);
    }

    /**
     * @notice Get the current registry version
     * @return Current version number
     */
    function getRegistryVersion() external view returns (uint256) {
        return feeRegistry.version();
    }

    /**
     * @notice Get chain name by ID
     * @param chainId The chain ID
     * @return Chain name string
     */
    function getChainName(uint8 chainId) external view returns (string memory) {
        return feeRegistry.getChainName(chainId);
    }

    // ============================================================
    // Required Overrides
    // ============================================================

    function votingDelay()
        public
        view
        override(Governor, GovernorSettings)
        returns (uint256)
    {
        return super.votingDelay();
    }

    function votingPeriod()
        public
        view
        override(Governor, GovernorSettings)
        returns (uint256)
    {
        return super.votingPeriod();
    }

    function quorum(uint256 blockNumber)
        public
        view
        override(Governor, GovernorVotesQuorumFraction)
        returns (uint256)
    {
        return super.quorum(blockNumber);
    }

    function state(uint256 proposalId)
        public
        view
        override(Governor, GovernorTimelockControl)
        returns (ProposalState)
    {
        return super.state(proposalId);
    }

    function proposalNeedsQueuing(uint256 proposalId)
        public
        view
        override(Governor, GovernorTimelockControl)
        returns (bool)
    {
        return super.proposalNeedsQueuing(proposalId);
    }

    function proposalThreshold()
        public
        view
        override(Governor, GovernorSettings)
        returns (uint256)
    {
        return super.proposalThreshold();
    }

    function _queueOperations(
        uint256 proposalId,
        address[] memory targets,
        uint256[] memory values,
        bytes[] memory calldatas,
        bytes32 descriptionHash
    ) internal override(Governor, GovernorTimelockControl) returns (uint48) {
        return super._queueOperations(proposalId, targets, values, calldatas, descriptionHash);
    }

    function _executeOperations(
        uint256 proposalId,
        address[] memory targets,
        uint256[] memory values,
        bytes[] memory calldatas,
        bytes32 descriptionHash
    ) internal override(Governor, GovernorTimelockControl) {
        super._executeOperations(proposalId, targets, values, calldatas, descriptionHash);
    }

    function _cancel(
        address[] memory targets,
        uint256[] memory values,
        bytes[] memory calldatas,
        bytes32 descriptionHash
    ) internal override(Governor, GovernorTimelockControl) returns (uint256) {
        return super._cancel(targets, values, calldatas, descriptionHash);
    }

    function _executor()
        internal
        view
        override(Governor, GovernorTimelockControl)
        returns (address)
    {
        return super._executor();
    }
}
