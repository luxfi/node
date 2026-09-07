// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package errors_test

import (
	stderrors "errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	nodeerrors "github.com/luxfi/node/errors"
)

// tempErr is a test helper that implements the optional Temporary() interface
// exercised by IsTemporary.
type tempErr struct {
	msg  string
	temp bool
}

func (t *tempErr) Error() string   { return t.msg }
func (t *tempErr) Temporary() bool { return t.temp }

func TestWrappedError_ErrorFormat(t *testing.T) {
	require := require.New(t)

	base := stderrors.New("disk gone")

	withMsg := nodeerrors.Wrap(base, nodeerrors.CategoryDatabase, "open failed")
	require.Equal("[database] open failed: disk gone", withMsg.Error())

	// Empty message — formatter omits the message portion.
	emptyMsg := nodeerrors.Wrap(base, nodeerrors.CategoryNetwork, "")
	require.Equal("[network] disk gone", emptyMsg.Error())
}

func TestWrap_NilPassThrough(t *testing.T) {
	require.Nil(t, nodeerrors.Wrap(nil, nodeerrors.CategoryDatabase, "ignored"))
	require.Nil(t, nodeerrors.WrapWithContext(nil, nodeerrors.CategoryDatabase, "ignored", map[string]interface{}{"k": "v"}))
}

func TestWrap_AllocatesContextMap(t *testing.T) {
	wrapped := nodeerrors.Wrap(stderrors.New("x"), nodeerrors.CategoryState, "msg")
	we := asWrapped(t, wrapped)
	require.NotNil(t, we.Context)
	// Should be writable without panic.
	we.Context["k"] = "v"
	require.Equal(t, "v", we.Context["k"])
}

func TestWrapWithContext_PreservesContext(t *testing.T) {
	ctx := map[string]interface{}{"tx_id": "0xabc", "height": uint64(42)}
	wrapped := nodeerrors.WrapWithContext(nodeerrors.ErrCorrupted, nodeerrors.CategoryDatabase, "block load", ctx)

	we := asWrapped(t, wrapped)
	require.Equal(t, ctx, we.Context)
	require.Equal(t, nodeerrors.CategoryDatabase, we.Category)
	require.Equal(t, "block load", we.Message)
}

func TestWrappedError_UnwrapAndIs(t *testing.T) {
	require := require.New(t)

	wrapped := nodeerrors.Wrap(nodeerrors.ErrNotFound, nodeerrors.CategoryDatabase, "lookup")
	require.True(stderrors.Is(wrapped, nodeerrors.ErrNotFound))
	require.False(stderrors.Is(wrapped, nodeerrors.ErrClosed))

	// errors.As must reach the WrappedError.
	var target *nodeerrors.WrappedError
	require.True(stderrors.As(wrapped, &target))
	require.Equal(nodeerrors.ErrNotFound, target.Unwrap())
}

func TestWrappedError_DoubleWrapChain(t *testing.T) {
	require := require.New(t)

	inner := nodeerrors.Wrap(nodeerrors.ErrTimeout, nodeerrors.CategoryNetwork, "dial")
	outer := nodeerrors.Wrap(inner, nodeerrors.CategoryNetwork, "rpc")

	// Sentinel survives through both wrap layers via Unwrap chain.
	require.True(stderrors.Is(outer, nodeerrors.ErrTimeout))
	require.True(nodeerrors.IsTimeout(outer))
}

func TestIsNotFound(t *testing.T) {
	require := require.New(t)
	require.True(nodeerrors.IsNotFound(nodeerrors.ErrNotFound))
	require.True(nodeerrors.IsNotFound(fmt.Errorf("lookup: %w", nodeerrors.ErrNotFound)))
	require.False(nodeerrors.IsNotFound(nodeerrors.ErrClosed))
	require.False(nodeerrors.IsNotFound(nil))
}

func TestIsClosed(t *testing.T) {
	require := require.New(t)
	require.True(nodeerrors.IsClosed(nodeerrors.ErrClosed))
	require.True(nodeerrors.IsClosed(fmt.Errorf("shutdown: %w", nodeerrors.ErrClosed)))
	require.False(nodeerrors.IsClosed(nodeerrors.ErrTimeout))
	require.False(nodeerrors.IsClosed(nil))
}

func TestIsTimeout(t *testing.T) {
	require := require.New(t)
	require.True(nodeerrors.IsTimeout(nodeerrors.ErrTimeout))
	require.True(nodeerrors.IsTimeout(fmt.Errorf("connect: %w", nodeerrors.ErrTimeout)))
	require.False(nodeerrors.IsTimeout(nodeerrors.ErrNotFound))
	require.False(nodeerrors.IsTimeout(nil))
}

func TestIsTemporary_SentinelErrors(t *testing.T) {
	require := require.New(t)

	require.True(nodeerrors.IsTemporary(nodeerrors.ErrTimeout))
	require.True(nodeerrors.IsTemporary(nodeerrors.ErrRateLimited))
	require.True(nodeerrors.IsTemporary(nodeerrors.ErrResourceExhausted))

	// Permanent / unrelated sentinels are not temporary.
	require.False(nodeerrors.IsTemporary(nodeerrors.ErrNotSupported))
	require.False(nodeerrors.IsTemporary(nodeerrors.ErrInvalidInput))
	require.False(nodeerrors.IsTemporary(stderrors.New("random")))
}

func TestIsTemporary_TemporaryInterface(t *testing.T) {
	require := require.New(t)

	require.True(nodeerrors.IsTemporary(&tempErr{msg: "blip", temp: true}))
	require.False(nodeerrors.IsTemporary(&tempErr{msg: "fatal", temp: false}))
}

func TestIsPermanent(t *testing.T) {
	require := require.New(t)

	permanent := []error{
		nodeerrors.ErrNotSupported,
		nodeerrors.ErrDeprecated,
		nodeerrors.ErrInvalidInput,
		nodeerrors.ErrInvalidSignature,
		nodeerrors.ErrInvalidFormat,
		nodeerrors.ErrForbidden,
		nodeerrors.ErrUnauthorized,
	}
	for _, err := range permanent {
		require.Truef(nodeerrors.IsPermanent(err), "expected permanent: %v", err)
	}

	require.False(nodeerrors.IsPermanent(nodeerrors.ErrTimeout))
	require.False(nodeerrors.IsPermanent(nodeerrors.ErrRateLimited))
	require.False(nodeerrors.IsPermanent(nil))

	// fmt.Errorf %w chain still resolves to permanent.
	require.True(nodeerrors.IsPermanent(fmt.Errorf("validate: %w", nodeerrors.ErrInvalidSignature)))
}

func TestGetCategory_FromWrappedError(t *testing.T) {
	require := require.New(t)
	wrapped := nodeerrors.Wrap(stderrors.New("x"), nodeerrors.CategoryResource, "oom")
	require.Equal(nodeerrors.CategoryResource, nodeerrors.GetCategory(wrapped))
}

func TestGetCategory_InferredFromSentinel(t *testing.T) {
	require := require.New(t)

	cases := []struct {
		err  error
		want nodeerrors.Category
	}{
		{nodeerrors.ErrNotFound, nodeerrors.CategoryDatabase},
		{nodeerrors.ErrClosed, nodeerrors.CategoryDatabase},
		{nodeerrors.ErrTimeout, nodeerrors.CategoryNetwork},
		{nodeerrors.ErrConnectionClosed, nodeerrors.CategoryNetwork},
		{nodeerrors.ErrInvalidInput, nodeerrors.CategoryValidation},
		{nodeerrors.ErrInvalidSignature, nodeerrors.CategoryValidation},
		{nodeerrors.ErrNotInitialized, nodeerrors.CategoryState},
		{nodeerrors.ErrAlreadyExists, nodeerrors.CategoryState},
		{nodeerrors.ErrResourceExhausted, nodeerrors.CategoryResource},
		{nodeerrors.ErrOutOfMemory, nodeerrors.CategoryResource},
		{nodeerrors.ErrUnauthorized, nodeerrors.CategoryPermission},
		{nodeerrors.ErrForbidden, nodeerrors.CategoryPermission},
		{stderrors.New("mystery"), nodeerrors.CategoryUnknown},
	}
	for _, tc := range cases {
		require.Equalf(tc.want, nodeerrors.GetCategory(tc.err),
			"category for %v", tc.err)
	}
}

func TestGetCategory_WrappedTakesPrecedenceOverInference(t *testing.T) {
	// ErrNotFound would normally infer CategoryDatabase. If wrapped with a
	// different explicit category, the wrapped value must win.
	wrapped := nodeerrors.Wrap(nodeerrors.ErrNotFound, nodeerrors.CategoryInternal, "ctx")
	require.Equal(t, nodeerrors.CategoryInternal, nodeerrors.GetCategory(wrapped))
}

func TestMulti_EmptyError(t *testing.T) {
	m := &nodeerrors.Multi{}
	require.Equal(t, "no errors", m.Error())
	require.Nil(t, m.Err())
}

func TestMulti_SingleErrorUnwrapsToInnerString(t *testing.T) {
	m := &nodeerrors.Multi{}
	m.Add(stderrors.New("only one"))
	require.Equal(t, "only one", m.Error())
	require.Equal(t, m, m.Err())
}

func TestMulti_MultipleErrorsContainsEach(t *testing.T) {
	m := &nodeerrors.Multi{}
	m.Add(stderrors.New("first"))
	m.Add(stderrors.New("second"))
	m.Add(stderrors.New("third"))

	s := m.Error()
	require.True(t, strings.Contains(s, "multiple errors"), "got: %s", s)
	require.True(t, strings.Contains(s, "first"))
	require.True(t, strings.Contains(s, "second"))
	require.True(t, strings.Contains(s, "third"))
}

func TestMulti_AddIgnoresNil(t *testing.T) {
	m := &nodeerrors.Multi{}
	m.Add(nil)
	m.Add(nil)
	require.Empty(t, m.Errors)
	require.Nil(t, m.Err())
}

func TestJoin_AllNilReturnsNil(t *testing.T) {
	require.Nil(t, nodeerrors.Join())
	require.Nil(t, nodeerrors.Join(nil, nil, nil))
}

func TestJoin_DropsNilEntries(t *testing.T) {
	err := nodeerrors.Join(nil, stderrors.New("real"), nil)
	require.NotNil(t, err)
	require.Equal(t, "real", err.Error())
}

func TestJoin_CombinesMultipleErrors(t *testing.T) {
	err := nodeerrors.Join(
		nodeerrors.ErrNotFound,
		nodeerrors.ErrClosed,
	)
	require.NotNil(t, err)
	s := err.Error()
	require.True(t, strings.Contains(s, "multiple errors"), "got: %s", s)
	require.True(t, strings.Contains(s, "not found"))
	require.True(t, strings.Contains(s, "closed"))
}

// asWrapped extracts a *nodeerrors.WrappedError or fails the test.
func asWrapped(t *testing.T, err error) *nodeerrors.WrappedError {
	t.Helper()
	var w *nodeerrors.WrappedError
	require.True(t, stderrors.As(err, &w), "expected *WrappedError, got %T", err)
	return w
}
