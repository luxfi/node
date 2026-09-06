# the AMM / DEX differential

One market — a constant-product pool and a limit order book over the same
ledger — deployed to the Go, C++ and Rust C-chains, driven through the same
script by the same three keys, with the resulting book put side by side.

```
go build ./conformance/dex && ./dex          # probe, run, and run the control
./dex -probe                                 # only report what each node admits to
./dex -only go,rust                          # a subset
./dex -fleet rust=http://127.0.0.1:23620/v1/chain/C   # where a node actually is
```

`-fleet` takes `lang=url`, comma separated, and replaces the URL of each
language named. A port is where a node happened to be started, not a property
of the implementation, so the defaults are a convenience and not a fact; a
language the fleet does not carry is refused rather than added, since a typo
would otherwise quietly run two of something.

Each node must have the signing key funded — see **the keys** below. Two of
the three ship a genesis that funds only a treasury Safe, and a chain that
cannot execute a transaction reports `includes=false` here and never leaves
height 0. `LLM.md` has the genesis that fixes it.

## what is compared, and why it is not the state root

The obvious thing to compare is the block's state root. It is the wrong thing.
Three chains with three genesis allocations and three sets of validators cannot
share a state root no matter how perfectly they agree, so a harness that
compared them would report a disagreement on every run and mean nothing by it.
The root is printed, and never compared.

What is compared is the **contract's** state: ten fixed storage slots, folded
into one word. Two of those slots are themselves folds:

| slot | what it holds |
| --- | --- |
| `fills` | every fill so far, folded **in match order** — maker, taker, price, quantity |
| `book` | every resting order, folded in array order |

`fills` is the load-bearing one. Matching order is consensus: two nodes that
produce the same set of fills in a different sequence hold different books and
will match the next order differently. A comparison of totals would call them
equal. `fills` cannot, because the fold is order-sensitive by construction.

The book is built so that order is decidable and observable — two asks rest at
the same price, so only arrival separates them, and a third rests better, so a
correct sweep must take price first and arrival second.

## the control

An agreement is only worth something if disagreement was reachable. The harness
runs the whole script a second time with the two equal-priced asks arriving in
the other order, and nothing else changed. The same quantity trades at the same
prices, so every count and every amount must come out identical and only the
sequence of makers differs.

If `fills` does not move under that, `fills` is not measuring matching order,
and the harness says the comparison is not evidence rather than claiming a pass.

## reading through storage, not through view functions

Every read is `eth_getStorageAt`. That is forced, not preferred. The three
implementations do not serve the same JSON-RPC:

| method | go | cpp | rust |
| --- | --- | --- | --- |
| `eth_sendRawTransaction` | yes | yes | yes |
| `eth_getStorageAt` / `getCode` / `getBalance` / `getTransactionCount` | yes | yes | yes |
| `eth_call` | yes | **no** | yes |
| `eth_estimateGas` | yes | **no** | yes |
| `eth_getTransactionReceipt` | yes | **no** | yes |
| `eth_getTransactionByHash` | yes | **no** | yes |
| `eth_getLogs` | yes | **no** | **no** |

A harness built on view functions would have quietly become a
two-implementation harness, and one built on receipts would have reported the
C++ chain as dead while it was mining perfectly well. Storage is the widest
surface all three answer on, and it is also the surface consensus is about.

Two consequences run through the code:

- **Inclusion is judged by the sender's nonce**, not by a receipt. A nonce that
  moved is the EVM's own statement that the transaction ran.
- The nonce asked for is the **accepted** one, never `pending`. A node stuck in
  bootstrap still admits transactions to its pool and still counts them in its
  pending nonce; asking for `pending` reports such a chain as live when it has
  accepted nothing. This is not hypothetical — it is what the first version of
  this harness did, and it called a bootstrapping Go cluster healthy.

## refusals

Three operations must be refused: a swap with no funds, an order with no funds,
and a self-trade. All three revert, and a reverted transaction spends its nonce
while leaving `fills` and `book` untouched. So a refusal is legible on every
implementation as *the nonce moved and the state did not* — no receipt needed.

## the keys

The three traders are the same three addresses in the same roles on every
chain: the first Anvil account, and `m/44'/60'/0'/0/{0,1}` off the published
light mnemonic. This is load-bearing. Both comparable digests fold `msg.sender`
into themselves — they must, since who owns a resting order is part of the book
— so letting each chain pick its own maker from whatever its genesis funded
produces three different folds and a **false** disagreement about matching
order. Which account *pays* is a separate question, answered per chain.
