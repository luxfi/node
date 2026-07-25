// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	apihealth "github.com/luxfi/api/health"
)

// The GET handler must emit the real check set. It previously encoded through
// jsonv2, which cannot represent apihealth.Result.Duration (a time.Duration),
// so every reply on every node degraded to {"healthy":…,"error":"health reply
// encode failed"} while the status code still looked correct — k8s probes
// passed and operators saw nothing.
func TestGetHandlerEncodesDurationBearingChecks(t *testing.T) {
	require := require.New(t)

	checks := map[string]apihealth.Result{
		"bls": {
			Details:   "node has the correct BLS key",
			Duration:  134597 * time.Nanosecond,
			Timestamp: time.Unix(1753479996, 0).UTC(),
		},
	}
	h := NewGetHandler(func(...string) (map[string]apihealth.Result, bool) {
		return checks, true
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/health", nil))

	require.Equal(http.StatusOK, rec.Code)

	var reply apihealth.APIReply
	require.NoError(json.Unmarshal(rec.Body.Bytes(), &reply))
	require.True(reply.Healthy)
	require.Len(reply.Checks, 1)
	require.Equal(134597*time.Nanosecond, reply.Checks["bls"].Duration)
	require.NotContains(rec.Body.String(), "health reply encode failed")
}

// An unhealthy node must still return the full diagnostic body alongside the
// 503 — the body is the only thing that says *why*.
func TestGetHandlerUnhealthyStillCarriesChecks(t *testing.T) {
	require := require.New(t)

	errMsg := "network layer is unhealthy reason: primary network validator has no inbound connections"
	checks := map[string]apihealth.Result{
		"network": {
			Details:            map[string]any{"connectedPeers": 4},
			Error:              &errMsg,
			Duration:           40357 * time.Nanosecond,
			ContiguousFailures: 31997,
		},
	}
	h := NewGetHandler(func(...string) (map[string]apihealth.Result, bool) {
		return checks, false
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/health", nil))

	require.Equal(http.StatusServiceUnavailable, rec.Code)

	var reply apihealth.APIReply
	require.NoError(json.Unmarshal(rec.Body.Bytes(), &reply))
	require.False(reply.Healthy)
	require.Equal(&errMsg, reply.Checks["network"].Error)
	require.Equal(int64(31997), reply.Checks["network"].ContiguousFailures)
}

// Check Details may embed raw chain-ID bytes. Invalid UTF-8 must degrade to
// replacement runes, never to an encode failure that drops the whole reply.
func TestGetHandlerInvalidUTF8InDetailsDoesNotDropReply(t *testing.T) {
	require := require.New(t)

	checks := map[string]apihealth.Result{
		"database": {
			Details:  string([]byte{0xff, 0xfe, 0x00}),
			Duration: 19137 * time.Nanosecond,
		},
	}
	h := NewGetHandler(func(...string) (map[string]apihealth.Result, bool) {
		return checks, true
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/health", nil))

	require.Equal(http.StatusOK, rec.Code)
	var reply apihealth.APIReply
	require.NoError(json.Unmarshal(rec.Body.Bytes(), &reply))
	require.Len(reply.Checks, 1)
	require.NotContains(rec.Body.String(), "health reply encode failed")
}
