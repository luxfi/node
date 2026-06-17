// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !dchain

// Package dexvm is, in the public (non-dchain) build, an INTENTIONALLY EMPTY
// backward-compatibility shim.
//
// The real alias (dexvm.go) re-exports github.com/luxfi/chains/dexvm and is
// gated behind the `dchain` build tag, because importing the D-Chain proxy tree
// would violate public-build purity (the public OSS luxd must not link
// chains/dexvm). This file exists only so the package has a buildable Go source
// in the public build — otherwise `go test ./...` would report "build
// constraints exclude all Go files" for this directory. Nothing in-tree imports
// this package; it is kept solely for out-of-tree backward compatibility under
// a venue (dchain) build.
package dexvm
