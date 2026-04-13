// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import "forge-std/Test.sol";
import "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import "../SolvencyStateMachine.sol";

/// @dev Minimal ERC20 for testing.
contract MockToken is ERC20 {
    constructor() ERC20("Wrapped ETH", "WETH") {}
    function mint(address to, uint256 amount) external { _mint(to, amount); }
}

contract SolvencyStateMachineTest is Test {
    SolvencyStateMachine ssm;
    MockToken weth;

    address owner = address(this);
    address alice = address(0xA);
    address bob   = address(0xB);

    uint256 constant TARGET_LTV = 7500;  // 75%
    uint256 constant MAX_LTV    = 8500;  // 85%
    uint256 constant LIQ_BONUS  = 500;   // 5%
    uint256 constant HALT_DELAY = 1 hours;

    function setUp() public {
        weth = new MockToken();
        ssm = new SolvencyStateMachine(
            IERC20(address(weth)),
            "Lux ETH",
            "LETH",
            TARGET_LTV,
            MAX_LTV,
            LIQ_BONUS,
            HALT_DELAY,
            owner
        );

        // Fund users
        weth.mint(alice, 100 ether);
        weth.mint(bob, 100 ether);

        vm.prank(alice);
        weth.approve(address(ssm), type(uint256).max);
        vm.prank(bob);
        weth.approve(address(ssm), type(uint256).max);
    }

    // ================================================================
    // Deposit + Mint
    // ================================================================

    function test_deposit_healthy() public {
        vm.prank(alice);
        ssm.deposit(10 ether, 5 ether); // 50% LTV -> Healthy

        assertEq(ssm.balanceOf(alice), 5 ether);
        assertEq(weth.balanceOf(address(ssm)), 10 ether);
        assertEq(uint256(ssm.vaultState(alice)), uint256(SolvencyStateMachine.AssetState.Healthy));
    }

    function test_deposit_warning_zone() public {
        vm.prank(alice);
        ssm.deposit(10 ether, 8 ether); // 80% LTV -> Warning (between 75% and 85%)

        assertEq(uint256(ssm.vaultState(alice)), uint256(SolvencyStateMachine.AssetState.Warning));
    }

    function test_deposit_at_max_ltv() public {
        vm.prank(alice);
        ssm.deposit(10 ether, 8.5 ether); // exactly 85% -> Warning (not > maxLTV)

        assertEq(ssm.vaultLTV(alice), MAX_LTV);
        // At exactly maxLTV: ltv <= maxLTV passes, ltv > targetLTV -> Warning
        assertEq(uint256(ssm.vaultState(alice)), uint256(SolvencyStateMachine.AssetState.Warning));
    }

    function test_deposit_exceeds_max_ltv_reverts() public {
        vm.prank(alice);
        vm.expectRevert("exceeds max LTV");
        ssm.deposit(10 ether, 8.6 ether); // 86% -> revert
    }

    function test_deposit_zero_reverts() public {
        vm.prank(alice);
        vm.expectRevert("zero deposit");
        ssm.deposit(0, 0);
    }

    function test_deposit_no_mint() public {
        vm.prank(alice);
        ssm.deposit(10 ether, 0); // deposit collateral only, no minting

        (uint256 collateral, uint256 debt) = ssm.vaults(alice);
        assertEq(collateral, 10 ether);
        assertEq(debt, 0);
        assertEq(ssm.balanceOf(alice), 0);
    }

    // ================================================================
    // Withdraw + Burn
    // ================================================================

    function test_withdraw_full() public {
        vm.startPrank(alice);
        ssm.deposit(10 ether, 5 ether);
        ssm.withdraw(5 ether);
        vm.stopPrank();

        assertEq(ssm.balanceOf(alice), 0);
        (uint256 collateral, uint256 debt) = ssm.vaults(alice);
        assertEq(collateral, 5 ether);
        assertEq(debt, 0);
    }

    function test_withdraw_partial() public {
        vm.startPrank(alice);
        ssm.deposit(10 ether, 5 ether); // 50% LTV
        ssm.withdraw(2 ether);           // removes 2 collateral + burns 2 credit
        vm.stopPrank();

        assertEq(ssm.balanceOf(alice), 3 ether);
        (uint256 collateral, uint256 debt) = ssm.vaults(alice);
        assertEq(collateral, 8 ether);
        assertEq(debt, 3 ether);
    }

    function test_withdraw_insufficient_collateral_reverts() public {
        vm.startPrank(alice);
        ssm.deposit(10 ether, 5 ether);
        vm.expectRevert("insufficient collateral");
        ssm.withdraw(11 ether);
        vm.stopPrank();
    }

    function test_withdraw_insufficient_debt_reverts() public {
        vm.startPrank(alice);
        ssm.deposit(10 ether, 2 ether);
        vm.expectRevert("insufficient debt");
        ssm.withdraw(3 ether);
        vm.stopPrank();
    }

    // ================================================================
    // Liquidation
    // ================================================================

    function test_liquidate_flow() public {
        // Alice deposits 10 WETH, mints 8 LETH (80% LTV = Warning)
        vm.prank(alice);
        ssm.deposit(10 ether, 8 ether);

        // Price drops: collateral now worth less -> pushes above maxLTV
        // At price = 0.9e18, LTV = 8 * 10000 * 1e18 / (10 * 0.9e18) = 8888 bps > 8500
        ssm.updatePrice(0.9e18);
        assertEq(uint256(ssm.vaultState(alice)), uint256(SolvencyStateMachine.AssetState.Liquidatable));

        // Bob needs LETH to liquidate. Mint some for bob via deposit.
        vm.prank(bob);
        ssm.deposit(20 ether, 8 ether);

        // Bob liquidates Alice
        vm.prank(bob);
        ssm.liquidate(alice);

        // Alice's vault is cleared
        (uint256 collateral, uint256 debt) = ssm.vaults(alice);
        assertEq(collateral, 0);
        assertEq(debt, 0);

        // Bob burned 8 LETH (Alice's debt) and received 10 WETH (Alice's collateral)
        assertEq(ssm.balanceOf(bob), 0); // 8 minted - 8 burned
        assertEq(weth.balanceOf(bob), 90 ether); // 100 - 20 deposited + 10 received
    }

    function test_liquidate_healthy_vault_reverts() public {
        vm.prank(alice);
        ssm.deposit(10 ether, 5 ether);

        vm.prank(bob);
        vm.expectRevert("not liquidatable");
        ssm.liquidate(alice);
    }

    function test_liquidate_warning_vault_reverts() public {
        vm.prank(alice);
        ssm.deposit(10 ether, 8 ether); // Warning zone

        vm.prank(bob);
        vm.expectRevert("not liquidatable");
        ssm.liquidate(alice);
    }

    // ================================================================
    // Halt / Resume
    // ================================================================

    function test_halt_requires_timelock() public {
        ssm.requestHalt();

        vm.expectRevert("timelock not elapsed");
        ssm.halt();

        vm.warp(block.timestamp + HALT_DELAY);
        ssm.halt();
        assertTrue(ssm.halted());
    }

    function test_halt_blocks_deposit() public {
        ssm.requestHalt();
        vm.warp(block.timestamp + HALT_DELAY);
        ssm.halt();

        vm.prank(alice);
        vm.expectRevert("halted");
        ssm.deposit(10 ether, 5 ether);
    }

    function test_halt_blocks_liquidation() public {
        vm.prank(alice);
        ssm.deposit(10 ether, 8 ether);
        ssm.updatePrice(0.9e18); // push to liquidatable

        ssm.requestHalt();
        vm.warp(block.timestamp + HALT_DELAY);
        ssm.halt();

        vm.prank(bob);
        vm.expectRevert("halted");
        ssm.liquidate(alice);
    }

    function test_withdraw_during_halt() public {
        vm.prank(alice);
        ssm.deposit(10 ether, 5 ether);

        ssm.requestHalt();
        vm.warp(block.timestamp + HALT_DELAY);
        ssm.halt();

        // Withdrawals still work during halt (users can exit)
        vm.prank(alice);
        ssm.withdraw(5 ether);
        assertEq(ssm.balanceOf(alice), 0);
    }

    function test_resume() public {
        ssm.requestHalt();
        vm.warp(block.timestamp + HALT_DELAY);
        ssm.halt();
        assertTrue(ssm.halted());

        ssm.resume();
        assertFalse(ssm.halted());

        // Deposits work again
        vm.prank(alice);
        ssm.deposit(10 ether, 5 ether);
        assertEq(ssm.balanceOf(alice), 5 ether);
    }

    function test_resume_when_not_halted_reverts() public {
        vm.expectRevert("not halted");
        ssm.resume();
    }

    function test_double_halt_reverts() public {
        ssm.requestHalt();
        vm.warp(block.timestamp + HALT_DELAY);
        ssm.halt();

        vm.expectRevert("already halted");
        ssm.requestHalt();
    }

    function test_cancel_halt() public {
        ssm.requestHalt();
        ssm.cancelHalt();
        assertEq(ssm.haltRequestedAt(), 0);
    }

    // ================================================================
    // LTV Boundary Conditions
    // ================================================================

    function test_ltv_at_target_is_healthy() public {
        vm.prank(alice);
        ssm.deposit(10 ether, 7.5 ether); // exactly 75% = targetLTV

        // ltv == targetLTV, not > targetLTV, so Healthy
        assertEq(ssm.vaultLTV(alice), TARGET_LTV);
        assertEq(uint256(ssm.vaultState(alice)), uint256(SolvencyStateMachine.AssetState.Healthy));
    }

    function test_ltv_just_above_target_is_warning() public {
        vm.prank(alice);
        ssm.deposit(10 ether, 7.501 ether);

        assertTrue(ssm.vaultLTV(alice) > TARGET_LTV);
        assertEq(uint256(ssm.vaultState(alice)), uint256(SolvencyStateMachine.AssetState.Warning));
    }

    function test_ltv_zero_collateral() public {
        assertEq(ssm.vaultLTV(alice), 0);
        assertEq(uint256(ssm.vaultState(alice)), uint256(SolvencyStateMachine.AssetState.Healthy));
    }

    function test_price_drop_triggers_liquidatable() public {
        vm.prank(alice);
        ssm.deposit(10 ether, 7 ether); // 70% LTV

        // Price drop from 1.0 to 0.8 -> new LTV = 70% / 0.8 = 87.5% > 85%
        ssm.updatePrice(0.8e18);
        assertEq(uint256(ssm.vaultState(alice)), uint256(SolvencyStateMachine.AssetState.Liquidatable));
    }

    // ================================================================
    // Invariants
    // ================================================================

    function test_system_ltv_invariant() public {
        // Multiple vaults, total credit must not exceed total collateral * maxLTV
        vm.prank(alice);
        ssm.deposit(10 ether, 8 ether);

        vm.prank(bob);
        ssm.deposit(10 ether, 8 ether);

        // Total: 20 WETH collateral, 16 LETH minted, system LTV = 80%
        assertEq(ssm.totalSupply(), 16 ether);
        assertEq(weth.balanceOf(address(ssm)), 20 ether);
    }

    function test_one_to_one_redeemability() public {
        vm.startPrank(alice);
        ssm.deposit(10 ether, 5 ether);

        uint256 balBefore = weth.balanceOf(alice);
        ssm.withdraw(5 ether); // burn 5 LETH -> get 5 WETH
        uint256 balAfter = weth.balanceOf(alice);
        vm.stopPrank();

        assertEq(balAfter - balBefore, 5 ether); // 1:1 redemption
    }

    // ================================================================
    // Access Control
    // ================================================================

    function test_only_owner_can_halt() public {
        vm.prank(alice);
        vm.expectRevert();
        ssm.requestHalt();
    }

    function test_only_owner_can_resume() public {
        ssm.requestHalt();
        vm.warp(block.timestamp + HALT_DELAY);
        ssm.halt();

        vm.prank(alice);
        vm.expectRevert();
        ssm.resume();
    }

    function test_only_owner_can_update_price() public {
        vm.prank(alice);
        vm.expectRevert();
        ssm.updatePrice(0.5e18);
    }
}
