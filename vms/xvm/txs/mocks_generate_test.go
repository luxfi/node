// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/tx.go -mock_names=UnsignedTx=UnsignedTx . UnsignedTx
