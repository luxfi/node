<<<<<<< HEAD
<<<<<<< HEAD
// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
=======
// Copyright (C) 2019-2022, Lux Partners Limited. All rights reserved.
>>>>>>> d7a7925ff (Update various imports)
=======
// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
>>>>>>> c5eafdb72 (Update LICENSE)
// See the file LICENSE for licensing terms.

package ids

// Equals returns true if the arrays are equal
func Equals(a, b []ID) bool {
	if len(a) != len(b) {
		return false
	}

	for i, aID := range a {
		if aID != b[i] {
			return false
		}
	}
	return true
}

// UnsortedEquals returns true if the have the same number of each ID
func UnsortedEquals(a, b []ID) bool {
	if len(a) != len(b) {
		return false
	}

	aBag := Bag{}
	aBag.Add(a...)

	bBag := Bag{}
	bBag.Add(b...)

	return aBag.Equals(bBag)
}
