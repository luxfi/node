// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"

	"github.com/luxfi/container/iterator"
)

func (s *state) GetExpiryIterator() (iterator.Iterator[ExpiryEntry], error) {
	return s.expiryDiff.getExpiryIterator(
		iterator.FromTree(s.expiry),
	), nil
}

// HasExpiry allows for concurrent reads.
func (s *state) HasExpiry(entry ExpiryEntry) (bool, error) {
	if has, modified := s.expiryDiff.modified[entry]; modified {
		return has, nil
	}
	return s.expiry.Has(entry), nil
}

func (s *state) PutExpiry(entry ExpiryEntry) {
	s.expiryDiff.PutExpiry(entry)
}

func (s *state) DeleteExpiry(entry ExpiryEntry) {
	s.expiryDiff.DeleteExpiry(entry)
}

func (s *state) loadExpiry() error {
	it := s.expiryDB.NewIterator()
	defer it.Release()

	for it.Next() {
		key := it.Key()

		var entry ExpiryEntry
		if err := entry.Unmarshal(key); err != nil {
			return fmt.Errorf("failed to unmarshal ExpiryEntry during load: %w", err)
		}
		s.expiry.ReplaceOrInsert(entry)
	}

	return nil
}

func (s *state) writeExpiry() error {
	for entry, isAdded := range s.expiryDiff.modified {
		var (
			key = entry.Marshal()
			err error
		)
		if isAdded {
			s.expiry.ReplaceOrInsert(entry)
			err = s.expiryDB.Put(key, nil)
		} else {
			s.expiry.Delete(entry)
			err = s.expiryDB.Delete(key)
		}
		if err != nil {
			return err
		}
	}

	s.expiryDiff = newExpiryDiff()
	return nil
}
