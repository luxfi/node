// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//go:build !cgo

package gpu

// A CGO_ENABLED=0 build has no way to open a shared library, so it never has a
// plugin. That is not a missing feature — the CPU backend is complete, and a
// node built this way computes every primitive itself. It is the build node2's
// own `make luxd RUNTIME=go` produces.

func pluginName() (string, bool) { return "", false }

func pluginKeccak256Batch(_ [][]byte) ([]Hash256, bool) { return nil, false }
