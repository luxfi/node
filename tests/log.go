// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"os"

	log "github.com/luxfi/log"
)

func NewDefaultLogger(prefix string) log.Logger {
	log, err := LoggerForFormat(prefix, "auto")
	if err != nil {
		// This should never happen since auto is a valid log format
		panic(err)
	}
	return log
}

// TODO(marun) Does/should the logging package have a function like this?
func LoggerForFormat(prefix string, rawLogFormat string) (log.Logger, error) {
	writeCloser := os.Stdout
	logFormat, err := log.ToFormat(rawLogFormat, writeCloser.Fd())
	if err != nil {
		return nil, err
	}
	return log.NewLogger(prefix, log.NewWrappedCore(log.Verbo, writeCloser, logFormat.ConsoleEncoder())), nil
}
