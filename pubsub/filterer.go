// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package pubsub

type Filterer interface {
	Filter(connections []Filter) ([]bool, interface{})
}
