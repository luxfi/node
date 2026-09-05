// SPDX-License-Identifier: BSD-3-Clause-Eco
pragma solidity 0.8.26;

/// One market, two halves: a constant-product pool and a limit order book over
/// the same two assets and the same internal ledger.
///
/// Everything a comparison needs is folded into fixed low-numbered slots,
/// because `eth_getStorageAt` is the only read method all three implementations
/// serve. A contract that answered through view functions would be readable on
/// two of the three, and a differential that skipped the third would not be one.
///
/// `fills` is the load-bearing word. It is folded in match order, so two nodes
/// that produce the same set of fills in a different sequence disagree on it.
/// A comparison of totals alone would call those two nodes equal, and they are
/// not: their books are in different states and their next match differs.
contract Dex {
    // -- slots 0..2: the pool ------------------------------------------------
    uint256 public poolBase;
    uint256 public poolQuote;
    uint256 public poolShares;

    // -- slots 3..9: the book ------------------------------------------------
    uint256 public nextId;
    uint256 public fillCount;
    bytes32 public fills; // order-sensitive fold: the matching sequence itself
    bytes32 public book;  // fold over the resting book, recomputed after each op
    uint256 public liveOrders;
    uint256 public refusals;
    uint256 public tradedQty;

    // -- slot 10: the ledger -------------------------------------------------
    struct Purse {
        uint128 base;
        uint128 quote;
        uint128 lockedBase;
        uint128 lockedQuote;
        uint256 shares;
    }
    mapping(address => Purse) public purse;

    // -- slot 11: the resting orders ----------------------------------------
    struct Order {
        address who;
        uint96 price; // quote per unit of base
        uint96 qty;   // base remaining
        bool buy;
        bool live;
    }
    Order[] public orders;

    // Refusal reasons. Reverting is the honest answer to a bad order, and the
    // reason is a string so a node that can report a revert reports why; a node
    // that cannot still shows the refusal, because a reverted transaction
    // leaves `fills` and `book` untouched while spending its nonce.
    error Unfunded();
    error SelfTrade();
    error NotYours();
    error Dead();
    error Empty();

    /// The traders are fixed at construction so every implementation starts
    /// from byte-identical state. Nothing here is minted later.
    constructor(address[] memory traders, uint128 base_, uint128 quote_) {
        for (uint256 i = 0; i < traders.length; i++) {
            purse[traders[i]].base = base_;
            purse[traders[i]].quote = quote_;
        }
        _reroot();
    }

    // ---------------------------------------------------------------- pool --

    function addLiquidity(uint128 base_, uint128 quote_) external returns (uint256 minted) {
        Purse storage p = purse[msg.sender];
        if (p.base < base_ || p.quote < quote_) revert Unfunded();
        p.base -= base_;
        p.quote -= quote_;

        minted = poolShares == 0
            ? _sqrt(uint256(base_) * uint256(quote_))
            : _min((uint256(base_) * poolShares) / poolBase, (uint256(quote_) * poolShares) / poolQuote);
        if (minted == 0) revert Empty();

        poolBase += base_;
        poolQuote += quote_;
        poolShares += minted;
        p.shares += minted;
    }

    function removeLiquidity(uint256 amount) external returns (uint128 base_, uint128 quote_) {
        Purse storage p = purse[msg.sender];
        if (p.shares < amount || amount == 0) revert Unfunded();

        base_ = uint128((amount * poolBase) / poolShares);
        quote_ = uint128((amount * poolQuote) / poolShares);

        p.shares -= amount;
        poolShares -= amount;
        poolBase -= base_;
        poolQuote -= quote_;
        p.base += base_;
        p.quote += quote_;
    }

    /// Constant product with a 30bp fee, the pricing every AMM in production
    /// uses. `buyBase` says which way the swap runs.
    function swap(bool buyBase, uint128 amountIn) external returns (uint128 out) {
        if (amountIn == 0) revert Empty();
        Purse storage p = purse[msg.sender];

        uint256 inReserve = buyBase ? poolQuote : poolBase;
        uint256 outReserve = buyBase ? poolBase : poolQuote;
        if (inReserve == 0 || outReserve == 0) revert Empty();

        if (buyBase) {
            if (p.quote < amountIn) revert Unfunded();
            p.quote -= amountIn;
        } else {
            if (p.base < amountIn) revert Unfunded();
            p.base -= amountIn;
        }

        uint256 net = (uint256(amountIn) * 997) / 1000;
        out = uint128((net * outReserve) / (inReserve + net));
        if (out == 0) revert Empty();

        if (buyBase) {
            poolQuote += amountIn;
            poolBase -= out;
            p.base += out;
        } else {
            poolBase += amountIn;
            poolQuote -= out;
            p.quote += out;
        }
    }

    // ---------------------------------------------------------------- book --

    /// Place a limit order. It matches against the opposite side first, by
    /// price then by arrival, and whatever survives rests.
    ///
    /// The scan is a linear walk of the order array in insertion order. That is
    /// not an efficiency choice: it makes the matching SEQUENCE a property of
    /// the code every implementation runs, rather than of a heap whose tie
    /// order an optimiser could permute. Two nodes cannot match this book in
    /// two orders and both be right.
    function place(bool buy, uint96 price, uint96 qty) external returns (uint256 id) {
        if (qty == 0 || price == 0) revert Empty();
        Purse storage taker = purse[msg.sender];

        uint96 remaining = qty;
        while (remaining > 0) {
            uint256 best = _best(!buy, price);
            if (best == type(uint256).max) break;

            Order storage m = orders[best];
            if (m.who == msg.sender) {
                refusals++;
                revert SelfTrade();
            }

            uint96 take = remaining < m.qty ? remaining : m.qty;
            uint256 cost = uint256(take) * uint256(m.price);

            // The taker pays at the maker's price, which is what a resting
            // order was promised.
            if (buy) {
                if (taker.quote < cost) {
                    refusals++;
                    revert Unfunded();
                }
                taker.quote -= uint128(cost);
                taker.base += take;
                Purse storage mk = purse[m.who];
                mk.lockedBase -= take;
                mk.quote += uint128(cost);
            } else {
                if (taker.base < take) {
                    refusals++;
                    revert Unfunded();
                }
                taker.base -= take;
                taker.quote += uint128(cost);
                Purse storage mk = purse[m.who];
                mk.lockedQuote -= uint128(cost);
                mk.base += take;
            }

            m.qty -= take;
            remaining -= take;
            if (m.qty == 0) {
                m.live = false;
                liveOrders--;
            }

            fills = keccak256(abi.encodePacked(fills, m.who, msg.sender, m.price, take));
            fillCount++;
            tradedQty += take;
        }

        if (remaining > 0) {
            // What rests must be paid for up front, so an order can never be
            // filled against money that was spent between placing and matching.
            if (buy) {
                uint256 lock = uint256(remaining) * uint256(price);
                if (taker.quote < lock) {
                    refusals++;
                    revert Unfunded();
                }
                taker.quote -= uint128(lock);
                taker.lockedQuote += uint128(lock);
            } else {
                if (taker.base < remaining) {
                    refusals++;
                    revert Unfunded();
                }
                taker.base -= remaining;
                taker.lockedBase += remaining;
            }
            id = orders.length;
            orders.push(Order({who: msg.sender, price: price, qty: remaining, buy: buy, live: true}));
            nextId++;
            liveOrders++;
        } else {
            id = type(uint256).max; // filled outright, nothing rests
        }
        _reroot();
    }

    function cancel(uint256 id) external {
        if (id >= orders.length) revert Dead();
        Order storage o = orders[id];
        if (!o.live) revert Dead();
        if (o.who != msg.sender) revert NotYours();

        Purse storage p = purse[msg.sender];
        if (o.buy) {
            uint128 lock = uint128(uint256(o.qty) * uint256(o.price));
            p.lockedQuote -= lock;
            p.quote += lock;
        } else {
            p.lockedBase -= o.qty;
            p.base += o.qty;
        }
        o.live = false;
        liveOrders--;
        _reroot();
    }

    /// The best resting order on `side` that a taker at `limit` would cross:
    /// best price first, and among equal prices the one that arrived first.
    function _best(bool side, uint96 limit) internal view returns (uint256) {
        uint256 found = type(uint256).max;
        uint96 bestPrice = 0;
        for (uint256 i = 0; i < orders.length; i++) {
            Order storage o = orders[i];
            if (!o.live || o.buy != side || o.qty == 0) continue;
            // A resting bid crosses a sell at or below its price; a resting ask
            // crosses a buy at or above its price.
            if (side ? o.price < limit : o.price > limit) continue;
            if (found == type(uint256).max || (side ? o.price > bestPrice : o.price < bestPrice)) {
                found = i;
                bestPrice = o.price;
            }
        }
        return found;
    }

    /// Fold the resting book, in array order, into one word.
    function _reroot() internal {
        bytes32 h = bytes32(0);
        for (uint256 i = 0; i < orders.length; i++) {
            Order storage o = orders[i];
            if (!o.live) continue;
            h = keccak256(abi.encodePacked(h, o.who, o.price, o.qty, o.buy));
        }
        book = h;
    }

    function _min(uint256 a, uint256 b) internal pure returns (uint256) { return a < b ? a : b; }

    function _sqrt(uint256 x) internal pure returns (uint256 y) {
        if (x == 0) return 0;
        uint256 z = (x + 1) / 2;
        y = x;
        while (z < y) { y = z; z = (x / z + z) / 2; }
    }
}
