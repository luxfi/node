// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package pcodecsmock provides a gomock-driven mock of the
// pcodecs.Manager interface (= proto/zap_codec.MultiManager) so
// node/vms test files can program codec expectations without
// importing proto/zap_codec or luxfi/codec directly.
//
// Wave 2G-Internal of the codec rip (#101) replaces the previous
// re-export of luxfi/codec/codecmock with an in-tree mock of the
// proto/zap_codec.MultiManager interface. The mock satisfies
// pcodecs.Manager by shape — RegisterCodec / Marshal / Unmarshal /
// Size — and is independent of the underlying codec wire format.
package pcodecsmock

import (
	"reflect"

	"github.com/luxfi/proto/zap_codec"
	gomock "go.uber.org/mock/gomock"
)

// Compile-time check: Manager satisfies zap_codec.MultiManager (the
// interface pcodecs.Manager aliases). Keeps the mock in sync with the
// underlying interface — any new method on MultiManager fails this
// assertion until the mock is updated.
var _ zap_codec.MultiManager = (*Manager)(nil)

// Manager is the gomock-driven mock of pcodecs.Manager (=
// zap_codec.MultiManager). Tests reach for pcodecsmock.NewManager(ctrl)
// to obtain a mock that satisfies pcodecs.Manager by shape.
type Manager struct {
	ctrl     *gomock.Controller
	recorder *ManagerRecorder
}

// ManagerRecorder is the gomock recorder for Manager. Tests program
// expectations via mock.EXPECT().<Method>(...).Return(...).
type ManagerRecorder struct {
	mock *Manager
}

// NewManager constructs a fresh Manager mock against the supplied
// gomock controller.
func NewManager(ctrl *gomock.Controller) *Manager {
	m := &Manager{ctrl: ctrl}
	m.recorder = &ManagerRecorder{mock: m}
	return m
}

// EXPECT returns the recorder so tests can program expectations.
func (m *Manager) EXPECT() *ManagerRecorder {
	return m.recorder
}

// RegisterCodec mocks the corresponding MultiManager method.
func (m *Manager) RegisterCodec(version uint16, codec zap_codec.Codec) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RegisterCodec", version, codec)
	ret0, _ := ret[0].(error)
	return ret0
}

// RegisterCodec expectation.
func (mr *ManagerRecorder) RegisterCodec(version, codec any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RegisterCodec", reflect.TypeOf((*Manager)(nil).RegisterCodec), version, codec)
}

// Marshal mocks the corresponding MultiManager method.
func (m *Manager) Marshal(version uint16, source interface{}) ([]byte, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Marshal", version, source)
	ret0, _ := ret[0].([]byte)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Marshal expectation.
func (mr *ManagerRecorder) Marshal(version, source any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Marshal", reflect.TypeOf((*Manager)(nil).Marshal), version, source)
}

// Unmarshal mocks the corresponding MultiManager method.
func (m *Manager) Unmarshal(source []byte, destination interface{}) (uint16, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Unmarshal", source, destination)
	ret0, _ := ret[0].(uint16)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Unmarshal expectation.
func (mr *ManagerRecorder) Unmarshal(source, destination any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Unmarshal", reflect.TypeOf((*Manager)(nil).Unmarshal), source, destination)
}

// Size mocks the corresponding MultiManager method.
func (m *Manager) Size(version uint16, value interface{}) (int, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Size", version, value)
	ret0, _ := ret[0].(int)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Size expectation.
func (mr *ManagerRecorder) Size(version, value any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Size", reflect.TypeOf((*Manager)(nil).Size), version, value)
}
