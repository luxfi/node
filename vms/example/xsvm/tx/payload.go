// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// Payload object layout: Sender 20B @0, Nonce u64 @20, IsReturn bool @28,
// Amount u64 @29, To 20B @37. size 57. Payload is a single type (not
// polymorphic), so it carries no kind byte.
const (
	offPayloadSender   = 0
	offPayloadNonce    = 20
	offPayloadIsReturn = 28
	offPayloadAmount   = 29
	offPayloadTo       = 37
	sizePayload        = 57
)

type Payload struct {
	// Sender + Nonce provides replay protection
	Sender   ids.ShortID `json:"sender"`
	Nonce    uint64      `json:"nonce"`
	IsReturn bool        `json:"isReturn"`
	Amount   uint64      `json:"amount"`
	To       ids.ShortID `json:"to"`

	bytes []byte
}

func (p *Payload) Bytes() []byte {
	return p.bytes
}

func (p *Payload) marshal() ([]byte, error) {
	b := zap.NewBuilder(zap.HeaderSize + sizePayload)
	ob := b.StartObject(sizePayload)
	ob.SetBytesFixed(offPayloadSender, p.Sender[:])
	ob.SetUint64(offPayloadNonce, p.Nonce)
	ob.SetBool(offPayloadIsReturn, p.IsReturn)
	ob.SetUint64(offPayloadAmount, p.Amount)
	ob.SetBytesFixed(offPayloadTo, p.To[:])
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func NewPayload(
	sender ids.ShortID,
	nonce uint64,
	isReturn bool,
	amount uint64,
	to ids.ShortID,
) (*Payload, error) {
	p := &Payload{
		Sender:   sender,
		Nonce:    nonce,
		IsReturn: isReturn,
		Amount:   amount,
		To:       to,
	}
	bytes, err := p.marshal()
	p.bytes = bytes
	return p, err
}

func ParsePayload(bytes []byte) (*Payload, error) {
	msg, err := zap.Parse(bytes)
	if err != nil {
		return nil, err
	}
	obj := msg.Root()
	p := &Payload{
		Nonce:    obj.Uint64(offPayloadNonce),
		IsReturn: obj.Bool(offPayloadIsReturn),
		Amount:   obj.Uint64(offPayloadAmount),
		bytes:    bytes,
	}
	copy(p.Sender[:], obj.BytesFixedSlice(offPayloadSender, shortLen))
	copy(p.To[:], obj.BytesFixedSlice(offPayloadTo, shortLen))
	return p, nil
}
