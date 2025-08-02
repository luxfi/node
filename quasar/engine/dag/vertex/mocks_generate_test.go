// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/vertex.go -mock_names=Vertex=Vertex . Vertex
//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/linearizable_vertex.go -mock_names=LinearizableVertex=LinearizableVertex . LinearizableVertex
//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/storage.go -mock_names=Storage=Storage . Storage
//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/manager.go -mock_names=Manager=Manager . Manager
//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/linearizable_vm.go -mock_names=LinearizableVM=LinearizableVM . LinearizableVM
//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/linearizable_vm_with_engine.go -mock_names=LinearizableVMWithEngine=LinearizableVMWithEngine . LinearizableVMWithEngine