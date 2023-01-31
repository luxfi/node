<<<<<<< HEAD
// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
=======
// Copyright (C) 2019-2022, Lux Partners Limited. All rights reserved.
>>>>>>> 53a8245a8 (Update consensus)
// See the file LICENSE for licensing terms.

package vertex

import (
	"bytes"
	"sort"

	"github.com/luxdefi/luxd/utils"
	"github.com/luxdefi/luxd/utils/hashing"
)

type sortHashOfData [][]byte

func (d sortHashOfData) Less(i, j int) bool {
	return bytes.Compare(
		hashing.ComputeHash256(d[i]),
		hashing.ComputeHash256(d[j]),
	) == -1
}
<<<<<<< HEAD

func (d sortHashOfData) Len() int {
	return len(d)
}

func (d sortHashOfData) Swap(i, j int) {
	d[j], d[i] = d[i], d[j]
}

func SortHashOf(bytesSlice [][]byte) {
	sort.Sort(sortHashOfData(bytesSlice))
}

=======
func (d sortHashOfData) Len() int      { return len(d) }
func (d sortHashOfData) Swap(i, j int) { d[j], d[i] = d[i], d[j] }

func SortHashOf(bytesSlice [][]byte) { sort.Sort(sortHashOfData(bytesSlice)) }
>>>>>>> 53a8245a8 (Update consensus)
func IsSortedAndUniqueHashOf(bytesSlice [][]byte) bool {
	return utils.IsSortedAndUnique(sortHashOfData(bytesSlice))
}
