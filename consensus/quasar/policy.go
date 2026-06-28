// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"fmt"

	qcert "github.com/luxfi/consensus/protocol/quasar"
)

// policyStore adapts the single configured QuasarEvidencePolicy to the consensus
// ConsensusCertPolicyStore interface. The verifier loads the required-leg set
// and the (kind, mode, param) permissions from HERE — never from the cert
// (invariants I1/I2). A cert that names a different PolicyID than the node's
// configured posture is rejected: a cert cannot pick its own weaker policy.
type policyStore struct{ policy *qcert.QuasarEvidencePolicy }

func (s policyStore) Policy(_ uint32, _ uint64, policyID uint32) (qcert.ConsensusCertPolicy, error) {
	if s.policy == nil {
		return nil, ErrPolicyUnavailable
	}
	if policyID != s.policy.EvidencePolicyID() {
		return nil, fmt.Errorf("%w: cert policy %d != configured %d", ErrPolicyMismatch, policyID, s.policy.EvidencePolicyID())
	}
	return s.policy, nil
}
