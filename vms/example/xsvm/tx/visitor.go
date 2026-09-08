// Copyright (C) 2023-2026, Lux Industries Inc. All rights reserved.
// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

type Visitor interface {
	Transfer(*Transfer) error
	Export(*Export) error
	Import(*Import) error
}
