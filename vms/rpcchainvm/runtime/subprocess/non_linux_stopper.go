// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !linux
// +build !linux

package subprocess

import (
	"context"
	"os/exec"

	"go.uber.org/zap"

	log "github.com/luxfi/log"
)

func NewCmd(path string, args ...string) *exec.Cmd {
	return exec.Command(path, args...)
}

func stop(_ context.Context, log log.Logger, cmd *exec.Cmd) {
	err := cmd.Process.Kill()
	if err == nil {
		log.Debug("subprocess was killed")
	} else {
		log.Error("subprocess was killed",
			zap.Error(err),
		)
	}
}
