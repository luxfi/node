// Copyright (C) 2019-2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

// Manager defines all the vertex related functionality that is required by the
// consensus engine.
type Manager interface {
	Builder
	Parser
	Storage
}
