// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build ignore

package throttling

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/luxfi/consensus/networking/tracker"
	"github.com/luxfi/ids"
	metric "github.com/luxfi/metric"
	"github.com/luxfi/node/utils/math/meter"
	"github.com/luxfi/node/utils/resource"
	"github.com/luxfi/node/utils/timer/mockable"
)

// mockTargeter implements tracker.Targeter for testing
type mockTargeter struct{}

func (m *mockTargeter) TargetUsage() uint64 {
	return 50 // Default target usage for testing (50%)
}

// mockResourceManager implements resource.Manager for testing
type mockResourceManager struct{}

func (m *mockResourceManager) CPUUsage() float64 {
	return 0.5
}

func (m *mockResourceManager) DiskUsage() (float64, float64) {
	return 0.5, 0.5
}

func (m *mockResourceManager) AvailableDiskBytes() uint64 {
	return 1000000000 // 1GB
}

func (m *mockResourceManager) TrackProcess(pid int) {
}

func (m *mockResourceManager) UntrackProcess(pid int) {
}

func (m *mockResourceManager) Shutdown() {}

func TestNewSystemThrottler(t *testing.T) {
	require := require.New(t)
	promReg := metric.NewNoOpMetrics("test").Registry()
	clock := mockable.Clock{}
	clock.Set(time.Now())
	resourceManager := &mockResourceManager{}
	resourceTracker, err := tracker.NewResourceTracker(promReg, resourceManager, time.Second)
	require.NoError(err)
	cpuTracker := resourceTracker.CPUTracker()

	config := SystemThrottlerConfig{
		Clock:           clock,
		MaxRecheckDelay: time.Second,
	}
	targeter := &mockTargeter{}
	throttlerIntf, err := NewSystemThrottler("", promReg, config, cpuTracker, targeter)
	require.NoError(err)
	require.IsType(&systemThrottler{}, throttlerIntf)
	throttler := throttlerIntf.(*systemThrottler)
	require.Equal(clock, config.Clock)
	require.Equal(time.Second, config.MaxRecheckDelay)
	require.Equal(cpuTracker, throttler.tracker)
	require.Equal(targeter, throttler.targeter)
}

func TestSystemThrottler(t *testing.T) {
	ctrl := gomock.NewController(t)
	require := require.New(t)

	// Setup
	mockTracker := trackermock.NewTracker(ctrl)
	maxRecheckDelay := 100 * time.Millisecond
	config := SystemThrottlerConfig{
		MaxRecheckDelay: maxRecheckDelay,
	}
	vdrID, nonVdrID := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	targeter := trackermock.NewTargeter(ctrl)
	throttler, err := NewSystemThrottler("", metric.NewNoOpMetrics("test").Registry(), config, mockTracker, targeter)
	require.NoError(err)

	// Case: Actual usage <= target usage; should return immediately
	// for both validator and non-validator
	targeter.EXPECT().TargetUsage(vdrID).Return(1.0).Times(1)
	mockTracker.EXPECT().Usage(gomock.Any(), vdrID).Return(0.9).Times(1)

	throttler.Acquire(context.Background(), vdrID)

	targeter.EXPECT().TargetUsage(nonVdrID).Return(1.0).Times(1)
	mockTracker.EXPECT().Usage(gomock.Any(), nonVdrID).Return(0.9).Times(1)

	throttler.Acquire(context.Background(), nonVdrID)

	// Case: Actual usage > target usage; we should wait.
	// In the first loop iteration inside acquire,
	// say the actual usage exceeds the target.
	targeter.EXPECT().TargetUsage(vdrID).Return(0.0).Times(1)
	mockTracker.EXPECT().Usage(gomock.Any(), vdrID).Return(1.0).Times(1)
	// Note we'll only actually wait [maxRecheckDelay]. We set [timeUntilAtDiskTarget]
	// much larger to assert that the min recheck frequency is honored below.
	timeUntilAtDiskTarget := 100 * maxRecheckDelay
	mockTracker.EXPECT().TimeUntilUsage(vdrID, gomock.Any(), gomock.Any()).Return(timeUntilAtDiskTarget).Times(1)

	// The second iteration, say the usage is OK.
	targeter.EXPECT().TargetUsage(vdrID).Return(1.0).Times(1)
	mockTracker.EXPECT().Usage(gomock.Any(), vdrID).Return(0.0).Times(1)

	onAcquire := make(chan struct{})

	// Check for validator
	go func() {
		throttler.Acquire(context.Background(), vdrID)
		onAcquire <- struct{}{}
	}()
	// Make sure the min re-check frequency is honored
	select {
	// Use 5*maxRecheckDelay and not just maxRecheckDelay to give a buffer
	// and avoid flakiness. If the min re-check freq isn't honored,
	// we'll wait [timeUntilAtDiskTarget].
	case <-time.After(5 * maxRecheckDelay):
		require.FailNow("should have returned after about [maxRecheckDelay]")
	case <-onAcquire:
	}

	targeter.EXPECT().TargetUsage(nonVdrID).Return(0.0).Times(1)
	mockTracker.EXPECT().Usage(gomock.Any(), nonVdrID).Return(1.0).Times(1)

	mockTracker.EXPECT().TimeUntilUsage(nonVdrID, gomock.Any(), gomock.Any()).Return(timeUntilAtDiskTarget).Times(1)

	targeter.EXPECT().TargetUsage(nonVdrID).Return(1.0).Times(1)
	mockTracker.EXPECT().Usage(gomock.Any(), nonVdrID).Return(0.0).Times(1)

	// Check for non-validator
	go func() {
		throttler.Acquire(context.Background(), nonVdrID)
		onAcquire <- struct{}{}
	}()
	// Make sure the min re-check frequency is honored
	select {
	// Use 5*maxRecheckDelay and not just maxRecheckDelay to give a buffer
	// and avoid flakiness. If the min re-check freq isn't honored,
	// we'll wait [timeUntilAtDiskTarget].
	case <-time.After(5 * maxRecheckDelay):
		require.FailNow("should have returned after about [maxRecheckDelay]")
	case <-onAcquire:
	}
}

func TestSystemThrottlerContextCancel(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	// Setup
	mockTracker := trackermock.NewTracker(ctrl)
	maxRecheckDelay := 10 * time.Second
	config := SystemThrottlerConfig{
		MaxRecheckDelay: maxRecheckDelay,
	}
	vdrID := ids.GenerateTestNodeID()
	targeter := trackermock.NewTargeter(ctrl)
	throttler, err := NewSystemThrottler("", metric.NewNoOpMetrics("test").Registry(), config, mockTracker, targeter)
	require.NoError(err)

	// Case: Actual usage > target usage; we should wait.
	// Mock the tracker so that the first loop iteration inside acquire,
	// it says the actual usage exceeds the target.
	// There should be no second iteration because we've already returned.
	targeter.EXPECT().TargetUsage(vdrID).Return(0.0).Times(1)
	mockTracker.EXPECT().Usage(gomock.Any(), vdrID).Return(1.0).Times(1)
	mockTracker.EXPECT().TimeUntilUsage(vdrID, gomock.Any(), gomock.Any()).Return(maxRecheckDelay).Times(1)
	onAcquire := make(chan struct{})
	// Pass a canceled context into Acquire so that it returns immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	go func() {
		throttler.Acquire(ctx, vdrID)
		onAcquire <- struct{}{}
	}()
	select {
	case <-onAcquire:
	case <-time.After(maxRecheckDelay / 2):
		// Make sure Acquire returns well before the second check (i.e. "immediately")
		require.Fail("should have returned immediately")
	}
}

// Mock implementations for testing (replaces trackermock package)

// MockTargeter is a mock implementation of tracker.Targeter
type MockTargeter struct {
	ctrl       *gomock.Controller
	recorder   *MockTargeterMockRecorder
}

// MockTargeterMockRecorder is the mock recorder for MockTargeter
type MockTargeterMockRecorder struct {
	mock *MockTargeter
}

// NewTargeter creates a mock targeter
func NewTargeter(ctrl *gomock.Controller) *MockTargeter {
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

// MockTracker is a mock implementation of tracker.Tracker  
type MockTracker struct {
	ctrl     *gomock.Controller
	recorder *MockTrackerMockRecorder
}

// MockTrackerMockRecorder is the mock recorder for MockTracker
type MockTrackerMockRecorder struct {
	mock *MockTracker
}

// NewTracker creates a mock tracker
func NewTracker(ctrl *gomock.Controller) *MockTracker {
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

// Create package-level aliases for the mocks
var (
	trackermock = struct {
		NewTargeter func(*gomock.Controller) *MockTargeter
		NewTracker  func(*gomock.Controller) *MockTracker
	}{
		NewTargeter: NewTargeter,
		NewTracker:  NewTracker,
	}
)
