# chains/go — Phase 2 placeholder

Empty. Reference: `~/work/lux/chains` — the Go chain SDK (`evm`, `quantumvm`,
`zkvm`, `dexvm`, `aivm`, `bridgevm`, `fhevm`, `graphvm`, `identityvm`,
`keyvm`, `mpcvm`, `oraclevm`, `relayvm`, `schain`), the port source for
`chains/rust` and `chains/cpp`.

`runtime/go` already builds one VM out of that reference directly
(`chains/evm`, see its README). What belongs here in Phase 2 is not a copy of
`luxfi/chains` — it is the P/X/C/Q/Z chain implementations poured into `luxd2`,
the clean Go node host named as the real gap in `runtime/go/README.md`, once
that host exists to load them into.
