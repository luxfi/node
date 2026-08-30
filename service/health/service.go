// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"github.com/luxfi/log"
)

// Service answers whether this node is up, ready and well. Its operations are
// registered by [Service.Ops].
type Service struct {
	log      log.Logger
	reporter Reporter
}

// NewService builds the health API over a reporter — the running check workers.
func NewService(log log.Logger, reporter Reporter) *Service {
	return &Service{log: log, reporter: reporter}
}
