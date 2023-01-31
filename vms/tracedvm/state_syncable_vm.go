// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracedvm

import (
	"context"

	"go.opentelemetry.io/otel/attribute"

	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

func (vm *blockVM) StateSyncEnabled(ctx context.Context) (bool, error) {
	if vm.ssVM == nil {
		return false, nil
	}

<<<<<<< HEAD
<<<<<<< HEAD
	ctx, span := vm.tracer.Start(ctx, vm.stateSyncEnabledTag)
=======
	ctx, span := vm.tracer.Start(ctx, "blockVM.StateSyncEnabled")
>>>>>>> c7cc22f98 (Add VM tracer (#2225))
=======
	ctx, span := vm.tracer.Start(ctx, vm.stateSyncEnabledTag)
>>>>>>> 37ccd9a48 (Add BuildBlockWithContext as an optional VM method (#2210))
	defer span.End()

	return vm.ssVM.StateSyncEnabled(ctx)
}

func (vm *blockVM) GetOngoingSyncStateSummary(ctx context.Context) (block.StateSummary, error) {
	if vm.ssVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

<<<<<<< HEAD
<<<<<<< HEAD
	ctx, span := vm.tracer.Start(ctx, vm.getOngoingSyncStateSummaryTag)
=======
	ctx, span := vm.tracer.Start(ctx, "blockVM.GetOngoingSyncStateSummary")
>>>>>>> c7cc22f98 (Add VM tracer (#2225))
=======
	ctx, span := vm.tracer.Start(ctx, vm.getOngoingSyncStateSummaryTag)
>>>>>>> 37ccd9a48 (Add BuildBlockWithContext as an optional VM method (#2210))
	defer span.End()

	return vm.ssVM.GetOngoingSyncStateSummary(ctx)
}

func (vm *blockVM) GetLastStateSummary(ctx context.Context) (block.StateSummary, error) {
	if vm.ssVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

<<<<<<< HEAD
<<<<<<< HEAD
	ctx, span := vm.tracer.Start(ctx, vm.getLastStateSummaryTag)
=======
	ctx, span := vm.tracer.Start(ctx, "blockVM.GetLastStateSummary")
>>>>>>> c7cc22f98 (Add VM tracer (#2225))
=======
	ctx, span := vm.tracer.Start(ctx, vm.getLastStateSummaryTag)
>>>>>>> 37ccd9a48 (Add BuildBlockWithContext as an optional VM method (#2210))
	defer span.End()

	return vm.ssVM.GetLastStateSummary(ctx)
}

func (vm *blockVM) ParseStateSummary(ctx context.Context, summaryBytes []byte) (block.StateSummary, error) {
	if vm.ssVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

<<<<<<< HEAD
<<<<<<< HEAD
	ctx, span := vm.tracer.Start(ctx, vm.parseStateSummaryTag, oteltrace.WithAttributes(
=======
	ctx, span := vm.tracer.Start(ctx, "blockVM.ParseStateSummary", oteltrace.WithAttributes(
>>>>>>> c7cc22f98 (Add VM tracer (#2225))
=======
	ctx, span := vm.tracer.Start(ctx, vm.parseStateSummaryTag, oteltrace.WithAttributes(
>>>>>>> 37ccd9a48 (Add BuildBlockWithContext as an optional VM method (#2210))
		attribute.Int("summaryLen", len(summaryBytes)),
	))
	defer span.End()

	return vm.ssVM.ParseStateSummary(ctx, summaryBytes)
}

func (vm *blockVM) GetStateSummary(ctx context.Context, height uint64) (block.StateSummary, error) {
	if vm.ssVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

<<<<<<< HEAD
<<<<<<< HEAD
	ctx, span := vm.tracer.Start(ctx, vm.getStateSummaryTag, oteltrace.WithAttributes(
=======
	ctx, span := vm.tracer.Start(ctx, "blockVM.GetStateSummary", oteltrace.WithAttributes(
>>>>>>> c7cc22f98 (Add VM tracer (#2225))
=======
	ctx, span := vm.tracer.Start(ctx, vm.getStateSummaryTag, oteltrace.WithAttributes(
>>>>>>> 37ccd9a48 (Add BuildBlockWithContext as an optional VM method (#2210))
		attribute.Int64("height", int64(height)),
	))
	defer span.End()

	return vm.ssVM.GetStateSummary(ctx, height)
}
