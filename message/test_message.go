<<<<<<< HEAD
// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
=======
// Copyright (C) 2019-2022, Lux Partners Limited. All rights reserved.
>>>>>>> d7a7925ff (Update various imports)
// See the file LICENSE for licensing terms.

package message

type TestMsg struct {
	op               Op
	bytes            []byte
	bypassThrottling bool
}

func NewTestMsg(op Op, bytes []byte, bypassThrottling bool) *TestMsg {
	return &TestMsg{
		op:               op,
		bytes:            bytes,
		bypassThrottling: bypassThrottling,
	}
}

func (m *TestMsg) Op() Op                   { return m.op }
func (*TestMsg) Get(Field) interface{}      { return nil }
func (m *TestMsg) Bytes() []byte            { return m.bytes }
func (*TestMsg) BytesSavedCompression() int { return 0 }
func (*TestMsg) AddRef()                    {}
func (*TestMsg) DecRef()                    {}
func (*TestMsg) IsProto() bool              { return false }
func (m *TestMsg) BypassThrottling() bool   { return m.bypassThrottling }
