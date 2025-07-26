// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package threshold

// terminationCondition defines the alpha confidence and beta thresholds
// required for a threshold instance to finalize
type terminationCondition struct {
	alphaConfidence int
	beta            int
}

func newSingleTerminationCondition(alphaConfidence int, beta int) []terminationCondition {
	return []terminationCondition{
		{
			alphaConfidence: alphaConfidence,
			beta:            beta,
		},
	}
}
