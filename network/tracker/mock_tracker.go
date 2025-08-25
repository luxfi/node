// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"reflect"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/luxfi/ids"
)

// MockTracker is a mock implementation of Tracker
type MockTracker struct {
	ctrl     *gomock.Controller
	recorder *MockTrackerMockRecorder
}

// MockTrackerMockRecorder is the mock recorder for MockTracker
type MockTrackerMockRecorder struct {
	mock *MockTracker
}

// NewMockTracker creates a new mock instance
func NewMockTracker(ctrl *gomock.Controller) *MockTracker {
	mock := &MockTracker{ctrl: ctrl}
	mock.recorder = &MockTrackerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use
func (m *MockTracker) EXPECT() *MockTrackerMockRecorder {
	return m.recorder
}

// Usage mocks base method
func (m *MockTracker) Usage(nodeID ids.NodeID, now time.Time) float64 {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Usage", nodeID, now)
	ret0, _ := ret[0].(float64)
	return ret0
}

// Usage indicates an expected call of Usage
func (mr *MockTrackerMockRecorder) Usage(nodeID, now interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Usage", reflect.TypeOf((*MockTracker)(nil).Usage), nodeID, now)
}

// TimeUntilUsage mocks base method
func (m *MockTracker) TimeUntilUsage(nodeID ids.NodeID, now time.Time, value float64) time.Duration {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "TimeUntilUsage", nodeID, now, value)
	ret0, _ := ret[0].(time.Duration)
	return ret0
}

// TimeUntilUsage indicates an expected call of TimeUntilUsage
func (mr *MockTrackerMockRecorder) TimeUntilUsage(nodeID, now, value interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "TimeUntilUsage", reflect.TypeOf((*MockTracker)(nil).TimeUntilUsage), nodeID, now, value)
}

// TotalUsage mocks base method
func (m *MockTracker) TotalUsage() float64 {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "TotalUsage")
	ret0, _ := ret[0].(float64)
	return ret0
}

// TotalUsage indicates an expected call of TotalUsage
func (mr *MockTrackerMockRecorder) TotalUsage() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "TotalUsage", reflect.TypeOf((*MockTracker)(nil).TotalUsage))
}

// MockTargeter is a mock implementation of Targeter
type MockTargeter struct {
	ctrl     *gomock.Controller
	recorder *MockTargeterMockRecorder
}

// MockTargeterMockRecorder is the mock recorder for MockTargeter
type MockTargeterMockRecorder struct {
	mock *MockTargeter
}

// NewMockTargeter creates a new mock instance
func NewMockTargeter(ctrl *gomock.Controller) *MockTargeter {
	mock := &MockTargeter{ctrl: ctrl}
	mock.recorder = &MockTargeterMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use
func (m *MockTargeter) EXPECT() *MockTargeterMockRecorder {
	return m.recorder
}

// TargetUsage mocks base method
func (m *MockTargeter) TargetUsage() uint64 {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "TargetUsage")
	ret0, _ := ret[0].(uint64)
	return ret0
}

// TargetUsage indicates an expected call of TargetUsage
func (mr *MockTargeterMockRecorder) TargetUsage() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "TargetUsage", reflect.TypeOf((*MockTargeter)(nil).TargetUsage))
}