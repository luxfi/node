<<<<<<< HEAD:vms/components/avax/mocks_generate_test.go
// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
=======
// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
>>>>>>> origin/regenesis-runtime-replay:vms/components/lux/lux/mocks_generate_test.go
// See the file LICENSE for licensing terms.

package lux

//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/transferable_in.go -mock_names=TransferableIn=TransferableIn . TransferableIn
