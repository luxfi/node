// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE} -destination=mock_diff.go . Diff
//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE} -destination=mock_state.go . State
//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE} -destination=mock_chain.go . Chain
//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE} -destination=mock_versions.go . Versions
