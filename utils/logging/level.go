// Copyright (C) 2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"fmt"
	"strings"
)

// Level represents a logging level
type Level int

const (
	Verbo Level = iota - 2
	Trace
	Debug
	Info
	Warn
	Error
	Fatal
	Off
)

var (
	levelStrings = map[Level]string{
		Verbo: "VERBO",
		Trace: "TRACE",
		Debug: "DEBUG",
		Info:  "INFO",
		Warn:  "WARN",
		Error: "ERROR",
		Fatal: "FATAL",
		Off:   "OFF",
	}

	levelStringsLower = map[Level]string{
		Verbo: "verbo",
		Trace: "trace",
		Debug: "debug",
		Info:  "info",
		Warn:  "warn",
		Error: "error",
		Fatal: "fatal",
		Off:   "off",
	}
)

// String returns the string representation of the level
func (l Level) String() string {
	if s, ok := levelStrings[l]; ok {
		return s
	}
	return "UNKNOWN"
}

// LowerString returns the lowercase string representation of the level
func (l Level) LowerString() string {
	if s, ok := levelStringsLower[l]; ok {
		return s
	}
	return "unknown"
}

// ToLevel converts a string to a Level
func ToLevel(s string) (Level, error) {
	switch strings.ToUpper(s) {
	case "VERBO":
		return Verbo, nil
	case "TRACE":
		return Trace, nil
	case "DEBUG":
		return Debug, nil
	case "INFO":
		return Info, nil
	case "WARN":
		return Warn, nil
	case "ERROR":
		return Error, nil
	case "FATAL":
		return Fatal, nil
	case "OFF":
		return Off, nil
	default:
		return Info, fmt.Errorf("unknown log level: %s", s)
	}
}