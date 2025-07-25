// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

type Agent[T TxID] struct {
	Issuer   Issuer[T]
	Listener Listener[T]
}

func NewAgent[T TxID](
	issuer Issuer[T],
	listener Listener[T],
) Agent[T] {
	return Agent[T]{
		Issuer:   issuer,
		Listener: listener,
	}
}
