// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	apihealth "github.com/luxfi/api/health"
)

// notYetRunResult is the result that is returned when a HealthCheck hasn't been
// run yet.
var notYetRunResult apihealth.Result

func init() {
	err := "not yet run"
	notYetRunResult = apihealth.Result{
		Error: &err,
	}
}
