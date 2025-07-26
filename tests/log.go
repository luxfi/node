// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"os"

	luxlog "github.com/luxfi/log"
)

func NewDefaultLogger(prefix string) luxlog.Logger {
	log, err := LoggerForFormat(prefix, logging.AutoString)
	if err != nil {
		// This should never happen since auto is a valid log format
		panic(err)
	}
	return log
}

// TODO(marun) Does/should the logging package have a function like this?
func LoggerForFormat(prefix string, rawLogFormat string) (luxlog.Logger, error) {
	writeCloser := os.Stdout
	logFormat, err := logging.ToFormat(rawLogFormat, writeCloser.Fd())
	if err != nil {
		return nil, err
	}
	return luxlog.NewZapLogger(prefix, logging.NewWrappedCore(luxlog.LevelVerbo, writeCloser, logFormat.ConsoleEncoder())), nil
}
