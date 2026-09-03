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
(lux-private lineage) — which is the one dependency this whole scaffold exists to
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
$ … | grep -E 'luxfi/node|lux-private'                            # zero matches
$ GOWORK=off go build ./evm                                    # exit 0, 68MB ELF
```

The other twelve import exactly five `luxfi/node` subpackages —
`config`, `version`, `vms`, `vms/artifacts`, `vms/types/fee` — the plugin SDK
surface a `ChainVM` needs to declare itself to a host, not networking or
consensus code. `lux-private` itself is absent everywhere in the module; the one
grep hit (`bridgevm/evmclient.go:12`) is a comment disclaiming it, not an
import.

## The actual gap (Phase 2, not this scaffold)

A clean Go node **host** — the thing that loads `chains/evm` and the other
plugins, dials peers, and drives `luxfi/consensus` — does not exist on disk
anywhere yet. Building one is new implementation work, which this
structural-shell pass does not do (see the repo root `LLM.md`). Its template
already exists, just not in Go: `~/work/lux-rs/node` is a clean,
`luxfi/node`-free, ZAP-native host that co-certifies with `luxd` today. A Go
host (`luxd2`) mirrors that architecture — plugin loader + P2P-over-ZAP +
bootstrap + `luxfi/consensus` — assembling components (`luxfi/consensus`,
`luxfi/zap`, `luxfi/chains`' VMs) that are each independently already clean or
cleanable. Extracting the five-package plugin-SDK surface those other twelve
VMs need out of `luxfi/node` and into a standalone package is the other half —
then all thirteen VMs are clean, not just one.

Until then, `bin/luxd-go` is `chains/evm` — a real, clean, verified artifact,
labeled for what it is.

See `~/work/lux/chains/PLUGGABLE.md` for the plugin architecture and
`~/work/lux/chains/README.md` for the full VM table.
