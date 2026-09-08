# runtime/go

The Go node is this repository. `make luxd RUNTIME=go` builds `./cmd/luxd` and
writes it to `bin/luxd-go`.

`runtime/rust` and `runtime/cpp` are shims because their source lives in another
checkout. This one names nothing, because there is nothing to name.
