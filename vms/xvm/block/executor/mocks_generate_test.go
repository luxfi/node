<<<<<<< HEAD:vms/avm/block/executor/mocks_generate_test.go
// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
=======
// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
>>>>>>> origin/regenesis-runtime-replay:vms/xvm/block/executor/mocks_generate_test.go
// See the file LICENSE for licensing terms.

package executor

//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/manager.go -mock_names=Manager=Manager . Manager
