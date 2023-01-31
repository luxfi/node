// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package set

import (
	"encoding/json"
	"fmt"
<<<<<<< HEAD
<<<<<<< HEAD
=======
	"math/rand"
>>>>>>> 87ce2da8a (Replace type specific sets with a generic implementation (#1861))
=======
>>>>>>> c334382d8 (Remove `testSettable` (#2364))
	"testing"

	"github.com/stretchr/testify/require"
)

<<<<<<< HEAD
<<<<<<< HEAD
func TestSet(t *testing.T) {
	require := require.New(t)
	id1 := 1

	s := Set[int]{id1: struct{}{}}
=======
func init() {
	rand.Seed(1337) // For determinism in generateTestSettable
}

type testSettable [20]byte

func (s testSettable) String() string {
	return fmt.Sprintf("%v", [20]byte(s))
}

func generateTestSettable() testSettable {
	var s testSettable
	_, _ = rand.Read(s[:]) // #nosec G404
	return s
}

=======
>>>>>>> c334382d8 (Remove `testSettable` (#2364))
func TestSet(t *testing.T) {
	require := require.New(t)
	id1 := 1

<<<<<<< HEAD
	s := Set[testSettable]{id1: struct{}{}}
>>>>>>> 87ce2da8a (Replace type specific sets with a generic implementation (#1861))
=======
	s := Set[int]{id1: struct{}{}}
>>>>>>> c334382d8 (Remove `testSettable` (#2364))

	s.Add(id1)
	require.True(s.Contains(id1))

	s.Remove(id1)
	require.False(s.Contains(id1))

	s.Add(id1)
	require.True(s.Contains(id1))
	require.Len(s.List(), 1)
	require.Equal(len(s.List()), 1)
	require.Equal(id1, s.List()[0])

	s.Clear()
	require.False(s.Contains(id1))

	s.Add(id1)

<<<<<<< HEAD
<<<<<<< HEAD
	s2 := Set[int]{}
=======
	s2 := Set[testSettable]{}
>>>>>>> 87ce2da8a (Replace type specific sets with a generic implementation (#1861))
=======
	s2 := Set[int]{}
>>>>>>> c334382d8 (Remove `testSettable` (#2364))

	require.False(s.Overlaps(s2))

	s2.Union(s)
	require.True(s2.Contains(id1))
	require.True(s.Overlaps(s2))

	s2.Difference(s)
	require.False(s2.Contains(id1))
	require.False(s.Overlaps(s2))
}

func TestSetCappedList(t *testing.T) {
	require := require.New(t)
<<<<<<< HEAD
<<<<<<< HEAD
	s := Set[int]{}

	id := 0
=======
	s := Set[testSettable]{}

	var id testSettable
>>>>>>> 87ce2da8a (Replace type specific sets with a generic implementation (#1861))
=======
	s := Set[int]{}

	id := 0
>>>>>>> c334382d8 (Remove `testSettable` (#2364))

	require.Len(s.CappedList(0), 0)

	s.Add(id)

	require.Len(s.CappedList(0), 0)
	require.Len(s.CappedList(1), 1)
	require.Equal(s.CappedList(1)[0], id)
	require.Len(s.CappedList(2), 1)
	require.Equal(s.CappedList(2)[0], id)

<<<<<<< HEAD
<<<<<<< HEAD
	id2 := 1
=======
	id2 := testSettable{1}
>>>>>>> 87ce2da8a (Replace type specific sets with a generic implementation (#1861))
=======
	id2 := 1
>>>>>>> c334382d8 (Remove `testSettable` (#2364))
	s.Add(id2)

	require.Len(s.CappedList(0), 0)
	require.Len(s.CappedList(1), 1)
	require.Len(s.CappedList(2), 2)
	require.Len(s.CappedList(3), 2)
	gotList := s.CappedList(2)
	require.Contains(gotList, id)
	require.Contains(gotList, id2)
	require.NotEqual(gotList[0], gotList[1])
}

func TestSetClear(t *testing.T) {
<<<<<<< HEAD
<<<<<<< HEAD
	set := Set[int]{}
	for i := 0; i < 25; i++ {
		set.Add(i)
	}
	set.Clear()
	require.Len(t, set, 0)
	set.Add(1337)
=======
	set := Set[testSettable]{}
=======
	set := Set[int]{}
>>>>>>> c334382d8 (Remove `testSettable` (#2364))
	for i := 0; i < 25; i++ {
		set.Add(i)
	}
	set.Clear()
	require.Len(t, set, 0)
<<<<<<< HEAD
	set.Add(generateTestSettable())
>>>>>>> 87ce2da8a (Replace type specific sets with a generic implementation (#1861))
=======
	set.Add(1337)
>>>>>>> c334382d8 (Remove `testSettable` (#2364))
	require.Len(t, set, 1)
}

func TestSetPop(t *testing.T) {
<<<<<<< HEAD
<<<<<<< HEAD
	var s Set[int]
	_, ok := s.Pop()
	require.False(t, ok)

	s = make(Set[int])
	_, ok = s.Pop()
	require.False(t, ok)

	id1, id2 := 0, 1
=======
	var s Set[testSettable]
=======
	var s Set[int]
>>>>>>> c334382d8 (Remove `testSettable` (#2364))
	_, ok := s.Pop()
	require.False(t, ok)

	s = make(Set[int])
	_, ok = s.Pop()
	require.False(t, ok)

<<<<<<< HEAD
	id1, id2 := generateTestSettable(), generateTestSettable()
>>>>>>> 87ce2da8a (Replace type specific sets with a generic implementation (#1861))
=======
	id1, id2 := 0, 1
>>>>>>> c334382d8 (Remove `testSettable` (#2364))
	s.Add(id1, id2)

	got, ok := s.Pop()
	require.True(t, ok)
	require.True(t, got == id1 || got == id2)
	require.EqualValues(t, 1, s.Len())

	got, ok = s.Pop()
	require.True(t, ok)
	require.True(t, got == id1 || got == id2)
	require.EqualValues(t, 0, s.Len())

	_, ok = s.Pop()
	require.False(t, ok)
}

func TestSetMarshalJSON(t *testing.T) {
	require := require.New(t)
<<<<<<< HEAD
<<<<<<< HEAD
	set := Set[int]{}
=======
	set := Set[testSettable]{}
>>>>>>> 87ce2da8a (Replace type specific sets with a generic implementation (#1861))
=======
	set := Set[int]{}
>>>>>>> c334382d8 (Remove `testSettable` (#2364))
	{
		asJSON, err := set.MarshalJSON()
		require.NoError(err)
		require.Equal("[]", string(asJSON))
	}
<<<<<<< HEAD
<<<<<<< HEAD
	id1, id2 := 1, 2
=======
	id1, id2 := testSettable{1}, testSettable{2}
>>>>>>> 87ce2da8a (Replace type specific sets with a generic implementation (#1861))
=======
	id1, id2 := 1, 2
>>>>>>> c334382d8 (Remove `testSettable` (#2364))
	id1JSON, err := json.Marshal(id1)
	require.NoError(err)
	id2JSON, err := json.Marshal(id2)
	require.NoError(err)
	set.Add(id1)
	{
		asJSON, err := set.MarshalJSON()
		require.NoError(err)
		require.Equal(fmt.Sprintf("[%s]", string(id1JSON)), string(asJSON))
	}
	set.Add(id2)
	{
		asJSON, err := set.MarshalJSON()
		require.NoError(err)
		require.Equal(fmt.Sprintf("[%s,%s]", string(id1JSON), string(id2JSON)), string(asJSON))
	}
}
