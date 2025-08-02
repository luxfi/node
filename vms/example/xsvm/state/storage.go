// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"

	db "github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/utils/math"
	"github.com/luxfi/node/v2/vms/platformvm/warp"
)

var (
	errWrongNonce          = errors.New("wrong nonce")
	errInsufficientBalance = errors.New("insufficient balance")
)

/*
 * VMDB
 * |-- initializedKey -> nil
 * |-. blocks
 * | |-- lastAcceptedKey -> blockID
 * | |-- height -> blockID
 * | '-- blockID -> block bytes
 * |-. addresses
 * | '-- addressID -> nonce
 * | '-- addressID + chainID -> balance
 * |-. chains
 * | |-- chainID -> balance
 * | '-- chainID + loanID -> nil
 * '-. message
 *   '-- txID -> message bytes
 */

// Chain state

func IsInitialized(database db.KeyValueReader) (bool, error) {
	return database.Has(initializedKey)
}

func SetInitialized(database db.KeyValueWriter) error {
	return database.Put(initializedKey, nil)
}

// Block state

func GetLastAccepted(database db.KeyValueReader) (ids.ID, error) {
	return db.GetID(database, blockPrefix)
}

func SetLastAccepted(database db.KeyValueWriter, blkID ids.ID) error {
	return db.PutID(database, blockPrefix, blkID)
}

func GetBlockIDByHeight(database db.KeyValueReader, height uint64) (ids.ID, error) {
	key := Flatten(blockPrefix, db.PackUInt64(height))
	return db.GetID(database, key)
}

func GetBlock(database db.KeyValueReader, blkID ids.ID) ([]byte, error) {
	key := Flatten(blockPrefix, blkID[:])
	return database.Get(key)
}

func AddBlock(database db.KeyValueWriter, height uint64, blkID ids.ID, blk []byte) error {
	heightToIDKey := Flatten(blockPrefix, db.PackUInt64(height))
	if err := db.PutID(database, heightToIDKey, blkID); err != nil {
		return err
	}
	idToBlockKey := Flatten(blockPrefix, blkID[:])
	return database.Put(idToBlockKey, blk)
}

// Address state

func GetNonce(database db.KeyValueReader, address ids.ShortID) (uint64, error) {
	key := Flatten(addressPrefix, address[:])
	return db.WithDefault(db.GetUInt64, database, key, 0)
}

func SetNonce(database db.KeyValueWriter, address ids.ShortID, nonce uint64) error {
	key := Flatten(addressPrefix, address[:])
	return db.PutUInt64(database, key, nonce)
}

func IncrementNonce(database db.KeyValueReaderWriter, address ids.ShortID, nonce uint64) error {
	expectedNonce, err := GetNonce(database, address)
	if err != nil {
		return err
	}
	if nonce != expectedNonce {
		return errWrongNonce
	}
	return SetNonce(database, address, nonce+1)
}

func GetBalance(database db.KeyValueReader, address ids.ShortID, chainID ids.ID) (uint64, error) {
	key := Flatten(addressPrefix, address[:], chainID[:])
	return db.WithDefault(db.GetUInt64, database, key, 0)
}

func SetBalance(database db.KeyValueWriterDeleter, address ids.ShortID, chainID ids.ID, balance uint64) error {
	key := Flatten(addressPrefix, address[:], chainID[:])
	if balance == 0 {
		return database.Delete(key)
	}
	return db.PutUInt64(database, key, balance)
}

func DecreaseBalance(database db.KeyValueReaderWriterDeleter, address ids.ShortID, chainID ids.ID, amount uint64) error {
	balance, err := GetBalance(database, address, chainID)
	if err != nil {
		return err
	}
	if balance < amount {
		return errInsufficientBalance
	}
	return SetBalance(database, address, chainID, balance-amount)
}

func IncreaseBalance(database db.KeyValueReaderWriterDeleter, address ids.ShortID, chainID ids.ID, amount uint64) error {
	balance, err := GetBalance(database, address, chainID)
	if err != nil {
		return err
	}
	balance, err = math.Add(balance, amount)
	if err != nil {
		return err
	}
	return SetBalance(database, address, chainID, balance)
}

// Chain state

func HasLoanID(database db.KeyValueReader, chainID ids.ID, loanID ids.ID) (bool, error) {
	key := Flatten(chainPrefix, chainID[:], loanID[:])
	return database.Has(key)
}

func AddLoanID(database db.KeyValueWriter, chainID ids.ID, loanID ids.ID) error {
	key := Flatten(chainPrefix, chainID[:], loanID[:])
	return database.Put(key, nil)
}

func GetLoan(database db.KeyValueReader, chainID ids.ID) (uint64, error) {
	key := Flatten(chainPrefix, chainID[:])
	return db.WithDefault(db.GetUInt64, database, key, 0)
}

func SetLoan(database db.KeyValueWriterDeleter, chainID ids.ID, balance uint64) error {
	key := Flatten(chainPrefix, chainID[:])
	if balance == 0 {
		return database.Delete(key)
	}
	return db.PutUInt64(database, key, balance)
}

func DecreaseLoan(database db.KeyValueReaderWriterDeleter, chainID ids.ID, amount uint64) error {
	balance, err := GetLoan(database, chainID)
	if err != nil {
		return err
	}
	if balance < amount {
		return errInsufficientBalance
	}
	return SetLoan(database, chainID, balance-amount)
}

func IncreaseLoan(database db.KeyValueReaderWriterDeleter, chainID ids.ID, amount uint64) error {
	balance, err := GetLoan(database, chainID)
	if err != nil {
		return err
	}
	balance, err = math.Add(balance, amount)
	if err != nil {
		return err
	}
	return SetLoan(database, chainID, balance)
}

// Message state

func GetMessage(database db.KeyValueReader, txID ids.ID) (*warp.UnsignedMessage, error) {
	key := Flatten(messagePrefix, txID[:])
	bytes, err := database.Get(key)
	if err != nil {
		return nil, err
	}
	return warp.ParseUnsignedMessage(bytes)
}

func SetMessage(database db.KeyValueWriter, txID ids.ID, message *warp.UnsignedMessage) error {
	key := Flatten(messagePrefix, txID[:])
	bytes := message.Bytes()
	return database.Put(key, bytes)
}
