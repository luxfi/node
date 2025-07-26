// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dynamicip

import luxlog "github.com/luxfi/log"

var _ Updater = noUpdater{}

func NewNoUpdater() Updater {
	return noUpdater{}
}

type noUpdater struct{}

func (noUpdater) Dispatch(luxlog.Logger) {}

func (noUpdater) Stop() {}
