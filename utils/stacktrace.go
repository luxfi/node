// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import "runtime"

func GetStacktrace(all bool) string {
	buf := make([]byte, 1<<16)
	n := runtime.Stack(buf, all)
	return string(buf[:n])
}
