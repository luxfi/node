// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import "fmt"

// Semantic represents a semantic version.
type Semantic struct {
	Major int
	Minor int
	Patch int
}

// String returns the string representation of the version.
func (v *Semantic) String() string {
	return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Compare returns:
// -1 if v < o
// 0 if v == o  
// 1 if v > o
func (v *Semantic) Compare(o *Semantic) int {
	if v.Major < o.Major {
		return -1
	}
	if v.Major > o.Major {
		return 1
	}
	if v.Minor < o.Minor {
		return -1
	}
	if v.Minor > o.Minor {
		return 1
	}
	if v.Patch < o.Patch {
		return -1
	}
	if v.Patch > o.Patch {
		return 1
	}
	return 0
}

// Before returns true if v < o
func (v *Semantic) Before(o *Semantic) bool {
	return v.Compare(o) < 0
}