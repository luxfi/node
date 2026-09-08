// Copyright (C) 2022-2026, Lux Industries Inc. All rights reserved.
// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dynamicip

import "github.com/luxfi/log"

var _ Updater = noUpdater{}

func NewNoUpdater() Updater {
	return noUpdater{}
}

type noUpdater struct{}

func (noUpdater) Dispatch(log.Logger) {}

func (noUpdater) Stop() {}
