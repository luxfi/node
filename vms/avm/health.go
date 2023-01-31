// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import "context"

// TODO: add health checks
<<<<<<< HEAD
func (*VM) HealthCheck(context.Context) (interface{}, error) {
=======
func (*VM) HealthCheck() (interface{}, error) {
>>>>>>> 707ffe48f (Add UnusedReceiver linter (#2224))
	return nil, nil
}
