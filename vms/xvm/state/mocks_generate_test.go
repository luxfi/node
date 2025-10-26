<<<<<<< HEAD:vms/avm/state/mocks_generate_test.go
// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
=======
// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
>>>>>>> origin/regenesis-runtime-replay:vms/xvm/state/mocks_generate_test.go
// See the file LICENSE for licensing terms.

package state

//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/chain.go -mock_names=Chain=Chain . Chain
//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/diff.go -mock_names=Diff=Diff . Diff
//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/state.go -mock_names=State=State . State
