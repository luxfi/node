// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build test
// +build test

package tmpnet

import (
	"context"
	"fmt"
)

// StartKindCluster is a stub implementation that doesn't use k8s.io packages
// The actual implementation is in start_kind_cluster.go.bak if k8s support is needed
func StartKindCluster(ctx context.Context) error {
	return fmt.Errorf("kind cluster support is disabled in this build")
}

// StopKindCluster stops the kind cluster
func StopKindCluster(ctx context.Context) error {
	return fmt.Errorf("kind cluster support is disabled in this build")
}