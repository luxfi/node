// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling
import (
	"github.com/luxfi/metric"
	"testing"
	"github.com/luxfi/mock/gomock"
	"github.com/stretchr/testify/require"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/message/messagemock"
	"github.com/luxfi/consensus/validators"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/log"
)
func TestSybilOutboundMsgThrottler(t *testing.T) {
	ctrl := gomock.NewController(t)
	require := require.New(t)
	config := MsgByteThrottlerConfig{
		VdrAllocSize:        1024,
		AtLargeAllocSize:    1024,
		NodeMaxAtLargeBytes: 1024,
	}
	vdrs := validators.NewManager()
	vdr1ID := ids.GenerateTestNodeID()
	vdr2ID := ids.GenerateTestNodeID()
	require.NoError(vdrs.AddStaker(constants.PrimaryNetworkID, vdr1ID, nil, ids.Empty, 1))
	require.NoError(vdrs.AddStaker(constants.PrimaryNetworkID, vdr2ID, nil, ids.Empty, 1))
	throttlerIntf, err := NewSybilOutboundMsgThrottler(
		log.NoLog{},
		metric.NewNoOp().Registry(),
		vdrs,
		config,
	)
	require.NoError(err)
	// Make sure NewSybilOutboundMsgThrottler works
	throttler := throttlerIntf.(*outboundMsgThrottler)
	require.Equal(config.VdrAllocSize, throttler.maxVdrBytes)
	require.Equal(config.VdrAllocSize, throttler.remainingVdrBytes)
	require.Equal(config.AtLargeAllocSize, throttler.remainingAtLargeBytes)
	require.NotNil(throttler.nodeToVdrBytesUsed)
	require.NotNil(throttler.log)
	require.NotNil(throttler.vdrs)
	// Take from at-large allocation.
	msg := testMsgWithSize(ctrl, 1)
	acquired := throttlerIntf.Acquire(msg, vdr1ID)
	require.True(acquired)
	require.Equal(config.AtLargeAllocSize-1, throttler.remainingAtLargeBytes)
	require.Empty(throttler.nodeToVdrBytesUsed)
	require.Len(throttler.nodeToAtLargeBytesUsed, 1)
	require.Equal(uint64(1), throttler.nodeToAtLargeBytesUsed[vdr1ID])
	// Release the bytes
	throttlerIntf.Release(msg, vdr1ID)
	require.Empty(throttler.nodeToAtLargeBytesUsed)
	// Use all the at-large allocation bytes and 1 of the validator allocation bytes
	msg = testMsgWithSize(ctrl, config.AtLargeAllocSize+1)
	acquired = throttlerIntf.Acquire(msg, vdr1ID)
	// vdr1 at-large bytes used: 1024. Validator bytes used: 1
	require.Zero(throttler.remainingAtLargeBytes)
	require.Equal(throttler.remainingVdrBytes, config.VdrAllocSize-1)
	require.Equal(uint64(1), throttler.nodeToVdrBytesUsed[vdr1ID])
	require.Len(throttler.nodeToVdrBytesUsed, 1)
	require.Equal(config.AtLargeAllocSize, throttler.nodeToAtLargeBytesUsed[vdr1ID])
	// The other validator should be able to acquire half the validator allocation.
	msg = testMsgWithSize(ctrl, config.AtLargeAllocSize/2)
	acquired = throttlerIntf.Acquire(msg, vdr2ID)
	// vdr2 at-large bytes used: 0. Validator bytes used: 512
	require.Equal(throttler.remainingVdrBytes, config.VdrAllocSize/2-1)
	require.Equal(uint64(1), throttler.nodeToVdrBytesUsed[vdr1ID], 1)
	require.Equal(config.VdrAllocSize/2, throttler.nodeToVdrBytesUsed[vdr2ID])
	require.Len(throttler.nodeToVdrBytesUsed, 2)
	// vdr1 should be able to acquire the rest of the validator allocation
	msg = testMsgWithSize(ctrl, config.VdrAllocSize/2-1)
	// vdr1 at-large bytes used: 1024. Validator bytes used: 512
	require.Equal(throttler.nodeToVdrBytesUsed[vdr1ID], config.VdrAllocSize/2)
	// Trying to take more bytes for either node should fail
	msg = testMsgWithSize(ctrl, 1)
	require.False(acquired)
	// Should also fail for non-validators
	acquired = throttlerIntf.Acquire(msg, ids.GenerateTestNodeID())
	// Release config.MaxAtLargeBytes+1 (1025) bytes
	// When the choice exists, bytes should be given back to the validator allocation
	// rather than the at-large allocation.
	// vdr1 at-large bytes used: 511. Validator bytes used: 0
	require.Equal(config.NodeMaxAtLargeBytes/2, throttler.remainingVdrBytes)
	require.Len(throttler.nodeToAtLargeBytesUsed, 1) // vdr1
	require.Equal(config.AtLargeAllocSize/2-1, throttler.nodeToAtLargeBytesUsed[vdr1ID])
	require.Equal(config.AtLargeAllocSize/2+1, throttler.remainingAtLargeBytes)
	// Non-validator should be able to take the rest of the at-large bytes
	// nonVdrID at-large bytes used: 513
	nonVdrID := ids.GenerateTestNodeID()
	msg = testMsgWithSize(ctrl, config.AtLargeAllocSize/2+1)
	acquired = throttlerIntf.Acquire(msg, nonVdrID)
	require.Equal(config.AtLargeAllocSize/2+1, throttler.nodeToAtLargeBytesUsed[nonVdrID])
	// Non-validator shouldn't be able to acquire more since at-large allocation empty
	// Release all of vdr2's messages
	throttlerIntf.Release(msg, vdr2ID)
	require.Zero(throttler.nodeToAtLargeBytesUsed[vdr2ID])
	// Release all of vdr1's messages
	require.Equal(config.AtLargeAllocSize/2-1, throttler.remainingAtLargeBytes)
	require.Zero(throttler.nodeToAtLargeBytesUsed[vdr1ID])
	// Release nonVdr's messages
	throttlerIntf.Release(msg, nonVdrID)
	require.Zero(throttler.nodeToAtLargeBytesUsed[nonVdrID])
}
// Ensure that the limit on taking from the at-large allocation is enforced
func TestSybilOutboundMsgThrottlerMaxNonVdr(t *testing.T) {
		VdrAllocSize:        100,
		AtLargeAllocSize:    100,
		NodeMaxAtLargeBytes: 10,
	nonVdrNodeID1 := ids.GenerateTestNodeID()
	msg := testMsgWithSize(ctrl, config.NodeMaxAtLargeBytes)
	acquired := throttlerIntf.Acquire(msg, nonVdrNodeID1)
	// Acquiring more should fail
	acquired = throttlerIntf.Acquire(msg, nonVdrNodeID1)
	// A different non-validator should be able to acquire
	nonVdrNodeID2 := ids.GenerateTestNodeID()
	msg = testMsgWithSize(ctrl, config.NodeMaxAtLargeBytes)
	acquired = throttlerIntf.Acquire(msg, nonVdrNodeID2)
	// Validator should only be able to take [MaxAtLargeBytes]
	msg = testMsgWithSize(ctrl, config.NodeMaxAtLargeBytes+1)
	throttlerIntf.Acquire(msg, vdr1ID)
	require.Equal(config.NodeMaxAtLargeBytes, throttler.nodeToAtLargeBytesUsed[vdr1ID])
	require.Equal(config.NodeMaxAtLargeBytes, throttler.nodeToAtLargeBytesUsed[nonVdrNodeID1])
	require.Equal(config.NodeMaxAtLargeBytes, throttler.nodeToAtLargeBytesUsed[nonVdrNodeID2])
	require.Equal(config.AtLargeAllocSize-config.NodeMaxAtLargeBytes*3, throttler.remainingAtLargeBytes)
// Ensure that the throttler honors requested bypasses
func TestBypassThrottling(t *testing.T) {
	msg := messagemock.NewOutboundMessage(ctrl)
	msg.EXPECT().BypassThrottling().Return(true).AnyTimes()
	msg.EXPECT().Op().Return(message.AppGossipOp).AnyTimes()
	msg.EXPECT().Bytes().Return(make([]byte, config.NodeMaxAtLargeBytes)).AnyTimes()
	// Acquiring more should not fail
	msg = messagemock.NewOutboundMessage(ctrl)
	msg.EXPECT().Bytes().Return(make([]byte, 1)).AnyTimes()
	msg2 := testMsgWithSize(ctrl, 1)
	acquired = throttlerIntf.Acquire(msg2, nonVdrNodeID1)
	msg.EXPECT().Bytes().Return(make([]byte, config.NodeMaxAtLargeBytes+1)).AnyTimes()
	require.Zero(throttler.nodeToVdrBytesUsed[vdr1ID])
	require.Equal(uint64(1), throttler.nodeToAtLargeBytesUsed[nonVdrNodeID1])
func testMsgWithSize(ctrl *gomock.Controller, size uint64) message.OutboundMessage {
	msg.EXPECT().BypassThrottling().Return(false).AnyTimes()
	msg.EXPECT().Bytes().Return(make([]byte, size)).AnyTimes()
	return msg
