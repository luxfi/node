// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xvm

import "context"

func (*VM) HealthCheck(context.Context) (interface{}, error) {
	return nil, nil
}
