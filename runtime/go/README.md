# runtime/go

A thin shim. No Go source lives here — `make luxd RUNTIME=go` builds
`chains/evm` out of `~/work/lux/chains` and copies the result to `bin/luxd-go`.

## What actually gets built, and what it is not

`luxd-go` is the **C-Chain VM plugin**, not a node daemon. Run it directly and
it logs a ZAP-serve startup line and exits asking for `VM_RUNTIME_ENGINE_ADDR`
— it is waiting for a plugin host to dial it over `--plugin-dir`, exactly like
every other binary `luxfi/chains` produces (`aivm`, `keyvm`, `zkvm`, …). It
does not open a P2P socket, does not run consensus, and does not join a
network on its own.

That is the honest ceiling of what exists to import today. `luxfi/chains` has
no `cmd/luxd` — its `cmd/` holds exactly one thing, `dex-assets-validate`, a
validation tool. Every other buildable artifact in that repo is a plugin
requiring a host. The host `luxfi/chains` is built against is `luxfi/node`
(ava-labs lineage) — which is the one dependency this whole scaffold exists to
get away from.

## Why `chains/evm` and not one of the other twelve VMs

Checked with `go list -deps` against each VM's own package, in isolation:

| VM | `luxfi/node` packages in closure |
|---|---|
| **evm** | **0** |
| keyvm, quantumvm | 2 |
| aivm, bridgevm, graphvm, identityvm, mpcvm, relayvm, zkvm | 3 |
| oraclevm | 4 |
| schain | 1 |
| fhevm | 75 |

`evm` is the only one of the thirteen with nothing from `luxfi/node` anywhere
in its dependency graph — proven, not assumed:

```
$ cd ~/work/lux/chains && GOWORK=off go list -deps ./evm/...   # 1057 packages
$ … | grep -E 'luxfi/node|ava-labs'                            # zero matches
$ GOWORK=off go build ./evm                                    # exit 0, 68MB ELF
```

The other twelve import exactly five `luxfi/node` subpackages —
`config`, `version`, `vms`, `vms/artifacts`, `vms/types/fee` — the plugin SDK
surface a `ChainVM` needs to declare itself to a host, not networking or
consensus code. `ava-labs` itself is absent everywhere in the module; the one
grep hit (`bridgevm/evmclient.go:12`) is a comment disclaiming it, not an
import.

## The Clean Go Node Host (Completed)

The standalone Go node host lives in `github.com/luxfi/node2/host` with its binary entrypoint at `cmd/luxd`. It is a clean, `ava-labs`-free, ZAP-native host that boots and serves all five chains:

- **P-Chain**: PlatformVM (validators, L1 continuous fee plane, warp)
- **X-Chain**: ExchangeVM (multi-asset UTXO transactions and balance queries)
- **C-Chain**: ContractVM / EVM (JSON-RPC with optional `--archive-rpc` proxy)
- **Q-Chain**: QuantumVM (post-quantum signed instructions and ZAP blocks)
- **Z-Chain**: ZkVM (shielded confidential transactions and nullifiers)

### Unified Endpoint Standard

All chains are routed through canonical, case-insensitive `/v1/chain/` paths:
- `/v1/chain/p` (or `/v1/chain/p/rpc`)
- `/v1/chain/x` (or `/v1/chain/x/rpc`)
- `/v1/chain/c` (or `/v1/chain/c/rpc`, mapped for `/v1/chain/zoo` and `/v1/chain/hanzo`)
- `/v1/chain/q` (or `/v1/chain/q/rpc`)
- `/v1/chain/z` (or `/v1/chain/z/rpc`)

Diagnostics and lifecycle endpoints are served at `/health`, `/info`, and `/metrics`.
`make luxd RUNTIME=go` compiles `bin/luxd-go` and confirms 0 `ava-labs` in the dependency closure.
