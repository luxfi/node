// Copyright (C) 2022-2026, Lux Industries Inc. All rights reserved.
// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import "runtime"

func GetStacktrace(all bool) string {
	buf := make([]byte, 1<<16)
	n := runtime.Stack(buf, all)
	return string(buf[:n])
}
