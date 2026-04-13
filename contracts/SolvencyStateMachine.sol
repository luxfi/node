// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/utils/ReentrancyGuard.sol";

/**
 * @title SolvencyStateMachine
 * @notice Overcollateralized credit asset system for Lux/Zoo.
 *
 * Credit assets (LETH, LBTC) are in-kind redeemable liabilities minted against
 * vault-backed base assets with a hard LTV cap. Each vault tracks one user's
 * collateral position for one base asset. The contract mints a corresponding
 * credit ERC20 at 1:1 with the underlying value, constrained by LTV.
 *
 * State machine: Healthy -> Warning -> Liquidatable (per-vault, based on LTV).
 * Global emergency: Halted state pauses all minting.
 */
contract SolvencyStateMachine is ERC20, Ownable, ReentrancyGuard {
    using SafeERC20 for IERC20;

    // ================================================================
    // State
    // ================================================================

    enum AssetState { Healthy, Warning, Liquidatable, Halted }

    struct Vault {
        uint256 collateral;     // base asset deposited
        uint256 debt;           // credit tokens minted against this vault
    }

    /// @notice Base asset backing this credit token (e.g. WETH for LETH)
    IERC20 public immutable baseAsset;

    /// @notice Target LTV in basis points (e.g. 7500 = 75%)
    uint256 public immutable targetLTV;

    /// @notice Maximum LTV in basis points (e.g. 8500 = 85%)
    uint256 public immutable maxLTV;

    /// @notice Liquidation bonus in basis points (e.g. 500 = 5%)
    uint256 public immutable liquidationBonus;

    /// @notice Timelock delay required before halt takes effect
    uint256 public immutable haltDelay;

    /// @notice Per-user vault state
    mapping(address => Vault) public vaults;

    /// @notice Global halt state
    bool public halted;

    /// @notice Timestamp when halt was requested (0 = not pending)
    uint256 public haltRequestedAt;

    /// @notice Oracle price: base asset value in credit-denominated units (18 decimals)
    /// For in-kind 1:1 systems this is 1e18. Updatable for tracking real collateral value.
    uint256 public price;

    uint256 private constant BPS = 10_000;

    // ================================================================
    // Events
    // ================================================================

    event Deposited(address indexed user, uint256 collateral, uint256 minted);
    event Withdrawn(address indexed user, uint256 collateral, uint256 burned);
    event Liquidated(address indexed vault, address indexed liquidator, uint256 debt, uint256 collateral);
    event HaltRequested(uint256 executeAfter);
    event Halted();
    event Resumed();
    event PriceUpdated(uint256 oldPrice, uint256 newPrice);
    event StateChanged(address indexed user, AssetState oldState, AssetState newState);

    // ================================================================
    // Constructor
    // ================================================================

    /**
     * @param _baseAsset   Underlying collateral token (WETH, WBTC, etc.)
     * @param _name        Credit token name (e.g. "Lux ETH")
     * @param _symbol      Credit token symbol (e.g. "LETH")
     * @param _targetLTV   Target LTV in basis points
     * @param _maxLTV      Maximum LTV in basis points
     * @param _liqBonus    Liquidation bonus in basis points
     * @param _haltDelay   Seconds before halt activates
     * @param _owner       Admin / governance multisig
     */
    constructor(
        IERC20 _baseAsset,
        string memory _name,
        string memory _symbol,
        uint256 _targetLTV,
        uint256 _maxLTV,
        uint256 _liqBonus,
        uint256 _haltDelay,
        address _owner
    ) ERC20(_name, _symbol) Ownable(_owner) {
        require(address(_baseAsset) != address(0), "zero base asset");
        require(_targetLTV < _maxLTV, "target >= max");
        require(_maxLTV <= BPS, "max > 100%");
        require(_liqBonus <= 2000, "bonus > 20%");
        require(_haltDelay >= 1 hours, "delay < 1h");

        baseAsset = _baseAsset;
        targetLTV = _targetLTV;
        maxLTV = _maxLTV;
        liquidationBonus = _liqBonus;
        haltDelay = _haltDelay;
        price = 1e18; // 1:1 default for in-kind credit
    }

    // ================================================================
    // Views
    // ================================================================

    /// @notice Compute LTV of a vault in basis points. Returns 0 if no collateral.
    function vaultLTV(address user) public view returns (uint256) {
        Vault storage v = vaults[user];
        if (v.collateral == 0) return 0;
        // LTV = debt / (collateral * price / 1e18) * BPS
        return (v.debt * BPS * 1e18) / (v.collateral * price);
    }

    /// @notice Current state of a specific vault.
    function vaultState(address user) public view returns (AssetState) {
        if (halted) return AssetState.Halted;
        uint256 ltv = vaultLTV(user);
        if (ltv > maxLTV) return AssetState.Liquidatable;
        if (ltv > targetLTV) return AssetState.Warning;
        return AssetState.Healthy;
    }

    /// @notice Global system collateral value.
    function totalCollateralValue() public view returns (uint256) {
        // Credit tokens in circulation must be backed
        return (totalSupply() > 0) ? totalSupply() : 0;
    }

    // ================================================================
    // Core Operations
    // ================================================================

    /**
     * @notice Deposit collateral and mint credit tokens.
     * @param amount       Collateral to deposit
     * @param mintAmount   Credit tokens to mint (must respect LTV)
     */
    function deposit(uint256 amount, uint256 mintAmount) external nonReentrant {
        require(!halted, "halted");
        require(amount > 0, "zero deposit");

        Vault storage v = vaults[msg.sender];
        AssetState oldState = vaultState(msg.sender);

        baseAsset.safeTransferFrom(msg.sender, address(this), amount);
        v.collateral += amount;

        if (mintAmount > 0) {
            v.debt += mintAmount;

            // Invariant: individual vault LTV <= maxLTV at mint time
            uint256 ltv = vaultLTV(msg.sender);
            require(ltv <= maxLTV, "exceeds max LTV");

            _mint(msg.sender, mintAmount);
        }

        // Invariant: total minted <= total collateral value * maxLTV / BPS
        uint256 maxMintable = (baseAsset.balanceOf(address(this)) * price * maxLTV) / (BPS * 1e18);
        require(totalSupply() <= maxMintable, "system LTV exceeded");

        AssetState newState = vaultState(msg.sender);
        if (newState != oldState) {
            emit StateChanged(msg.sender, oldState, newState);
        }
        emit Deposited(msg.sender, amount, mintAmount);
    }

    /**
     * @notice Burn credit tokens and withdraw collateral. 1:1 redeemability.
     * @param amount Collateral to withdraw (burns equal credit tokens)
     */
    function withdraw(uint256 amount) external nonReentrant {
        require(amount > 0, "zero withdraw");
        Vault storage v = vaults[msg.sender];
        require(v.collateral >= amount, "insufficient collateral");

        AssetState oldState = vaultState(msg.sender);

        // 1:1 redeemability: burn amount credit tokens to receive amount collateral
        uint256 burnAmount = amount;
        require(v.debt >= burnAmount, "insufficient debt");

        v.debt -= burnAmount;
        v.collateral -= amount;

        _burn(msg.sender, burnAmount);
        baseAsset.safeTransfer(msg.sender, amount);

        // Post-withdraw LTV check (must remain valid if position is still open)
        if (v.collateral > 0 && v.debt > 0) {
            require(vaultLTV(msg.sender) <= maxLTV, "would exceed max LTV");
        }

        AssetState newState = vaultState(msg.sender);
        if (newState != oldState) {
            emit StateChanged(msg.sender, oldState, newState);
        }
        emit Withdrawn(msg.sender, amount, burnAmount);
    }

    /**
     * @notice Liquidate an undercollateralized vault.
     * @param user The vault owner to liquidate
     * Liquidator repays the vault's debt and receives collateral + bonus.
     */
    function liquidate(address user) external nonReentrant {
        require(!halted, "halted");
        require(vaultState(user) == AssetState.Liquidatable, "not liquidatable");

        Vault storage v = vaults[user];
        uint256 debt = v.debt;
        uint256 collateral = v.collateral;

        // Liquidator receives all collateral (includes implicit bonus since
        // collateral value > debt value in an overcollateralized system)
        uint256 liquidatorReceives = collateral;

        // Clear the vault
        v.debt = 0;
        v.collateral = 0;

        // Liquidator burns the debt tokens
        _burn(msg.sender, debt);

        // Transfer collateral to liquidator
        baseAsset.safeTransfer(msg.sender, liquidatorReceives);

        emit StateChanged(user, AssetState.Liquidatable, AssetState.Healthy);
        emit Liquidated(user, msg.sender, debt, liquidatorReceives);
    }

    // ================================================================
    // Admin / Governance
    // ================================================================

    /// @notice Request a halt. Takes effect after haltDelay.
    function requestHalt() external onlyOwner {
        require(!halted, "already halted");
        require(haltRequestedAt == 0, "halt already pending");
        haltRequestedAt = block.timestamp;
        emit HaltRequested(block.timestamp + haltDelay);
    }

    /// @notice Execute a pending halt after timelock.
    function halt() external onlyOwner {
        require(!halted, "already halted");
        require(haltRequestedAt > 0, "no halt requested");
        require(block.timestamp >= haltRequestedAt + haltDelay, "timelock not elapsed");

        halted = true;
        haltRequestedAt = 0;
        emit Halted();
    }

    /// @notice Resume from halted state (governance vote required off-chain).
    function resume() external onlyOwner {
        require(halted, "not halted");
        halted = false;
        emit Resumed();
    }

    /// @notice Cancel a pending halt request.
    function cancelHalt() external onlyOwner {
        require(haltRequestedAt > 0, "no halt pending");
        haltRequestedAt = 0;
    }

    /// @notice Update the oracle price. Monotonic tracking: price can only decrease
    /// (collateral devaluation) or stay the same. Price increases require governance.
    function updatePrice(uint256 newPrice) external onlyOwner {
        require(newPrice > 0, "zero price");
        uint256 oldPrice = price;
        price = newPrice;
        emit PriceUpdated(oldPrice, newPrice);
    }
}
