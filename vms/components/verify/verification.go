// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package verify

import vmverify "github.com/luxfi/vm/components/verify"

type Verifiable = vmverify.Verifiable
type State = vmverify.State
type IsState = vmverify.IsState
type IsNotState = vmverify.IsNotState

// All returns nil if all the verifiables were verified with no errors
func All(verifiables ...Verifiable) error {
	return vmverify.All(verifiables...)
}
