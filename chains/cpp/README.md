# chains/cpp

The chain suite in C++, behind `~/work/lux-cpp/node`'s VM seam
(`include/lux/node/vm.hpp`). `runtime/cpp` already runs a C-Chain today via
`cevm` (see its README); this is the rest of the suite.

| chain | state |
|---|---|
| `xvm/` | **the X-Chain — here.** The UTXO DAG and asset transfers, ported from `~/work/lux/node/vms/xvm`. Wire bytes checked against the Go implementation's own; 11 test suites, 677 assertions. See `xvm/LLM.md`. |
| P, Q, Z | not started |

Unlike the rest of `node2`, this is real code rather than a shim: there was no
C++ X-Chain to import, so it was written. It depends on `luxcpp/crypto` and the
two seam headers, and on nothing else — no Go module is in its build.
