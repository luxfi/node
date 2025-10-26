<<<<<<< HEAD:vms/avm/health.go
// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
=======
// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
>>>>>>> origin/regenesis-runtime-replay:vms/xvm/health.go
// See the file LICENSE for licensing terms.

package xvm

import "context"

func (*VM) HealthCheck(context.Context) (interface{}, error) {
	return nil, nil
}
