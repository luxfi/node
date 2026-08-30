// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// The served health operation, held to what a probe and an operator both need.
//
// The regression these keep is a real one: the reply carries a time.Duration per
// check and details a check chose the shape of, and an encoder that could not
// represent either answered {"healthy":…,"error":"health reply encode failed"}
// with the status code still looking correct — so probes passed and operators
// saw nothing. The body is the only thing that says WHY, so it is asserted here
// rather than the status alone.

package health

import (
	"net/http"
	"testing"
	"time"

	"github.com/go-json-experiment/json"
	"github.com/stretchr/testify/require"

	apihealth "github.com/luxfi/api/health"
	apitypes "github.com/luxfi/api/types"
	"github.com/luxfi/log"
)

// stub answers with a fixed set of checks, so the only thing under test is what
// the operation does with them.
type stub struct {
	checks  map[string]apihealth.Result
	healthy bool
}

func (s stub) Readiness(...string) (map[string]apihealth.Result, bool) { return s.checks, s.healthy }
func (s stub) Health(...string) (map[string]apihealth.Result, bool)    { return s.checks, s.healthy }
func (s stub) Liveness(...string) (map[string]apihealth.Result, bool)  { return s.checks, s.healthy }

func serve(t *testing.T, checks map[string]apihealth.Result, healthy bool, path string) (*http.Response, string) {
	t.Helper()
	app := NewService(log.NewNoOpLogger(), stub{checks: checks, healthy: healthy}).Ops()
	t.Cleanup(func() { _ = app.Shutdown() })

	resp, err := app.Test(httptest(path))
	require.NoError(t, err)
	body := make([]byte, 1<<16)
	n, _ := resp.Body.Read(body)
	return resp, string(body[:n])
}

func httptest(path string) *http.Request {
	req, _ := http.NewRequest(http.MethodGet, "http://node"+path, nil)
	return req
}

func raw(t *testing.T, v any) apitypes.Raw {
	t.Helper()
	r, err := apitypes.RawOf(v)
	require.NoError(t, err)
	return r
}

// A healthy node answers 200 and the whole check set, durations intact.
func TestHealthyCarriesEveryCheck(t *testing.T) {
	require := require.New(t)

	resp, body := serve(t, map[string]apihealth.Result{
		"bls": {
			Details:   raw(t, "node has the correct BLS key"),
			Duration:  134597 * time.Nanosecond,
			Timestamp: apitypes.TimeOf(time.Unix(1753479996, 0).UTC()),
		},
	}, true, "/health")

	require.Equal(http.StatusOK, resp.StatusCode)
	require.NotContains(body, "health reply encode failed")

	var reply apihealth.APIReply
	require.NoError(json.Unmarshal([]byte(body), &reply))
	require.True(reply.Healthy)
	require.Len(reply.Checks, 1)
	check, ok := reply.Checks.Find("bls")
	require.True(ok)
	require.Equal(134597*time.Nanosecond, check.Result.Duration)
	require.JSONEq(`"node has the correct BLS key"`, string(check.Result.Details))
}

// An unhealthy node answers 503 AND the full diagnostic body — the status says
// something is wrong and only the body says what.
func TestUnhealthyIs503AndStillCarriesChecks(t *testing.T) {
	require := require.New(t)

	why := "network layer is unhealthy reason: primary network validator has no inbound connections"
	resp, body := serve(t, map[string]apihealth.Result{
		"network": {
			Details:            raw(t, map[string]any{"connectedPeers": 4}),
			Error:              &why,
			Duration:           40357 * time.Nanosecond,
			ContiguousFailures: 31997,
		},
	}, false, "/health")

	require.Equal(http.StatusServiceUnavailable, resp.StatusCode)

	var reply apihealth.APIReply
	require.NoError(json.Unmarshal([]byte(body), &reply))
	require.False(reply.Healthy)
	check, ok := reply.Checks.Find("network")
	require.True(ok)
	require.Equal(&why, check.Result.Error)
	require.Equal(int64(31997), check.Result.ContiguousFailures)
	require.JSONEq(`{"connectedPeers":4}`, string(check.Result.Details))
}

// Readiness and liveness are the probe addresses and answer the same two codes.
func TestEveryProbeAddressAnswers(t *testing.T) {
	for _, path := range []string{"/readiness", "/health", "/liveness"} {
		t.Run(path, func(t *testing.T) {
			resp, _ := serve(t, nil, true, path)
			require.Equal(t, http.StatusOK, resp.StatusCode)

			resp, _ = serve(t, nil, false, path)
			require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
		})
	}
}

// A check's details are whatever it said, and they reach a caller as those
// bytes. Three checks on mainnet answer with three different JSON shapes; a
// reply that could only carry one of them would be a reply that dropped two.
func TestDetailsKeepWhateverShapeTheCheckChose(t *testing.T) {
	require := require.New(t)

	resp, body := serve(t, map[string]apihealth.Result{
		"object": {Details: raw(t, map[string]any{"availableDiskBytes": 12})},
		"text":   {Details: raw(t, "node is not a validator")},
		"list":   {Details: raw(t, []string{"P", "X", "C"})},
	}, true, "/health")

	require.Equal(http.StatusOK, resp.StatusCode)

	var reply apihealth.APIReply
	require.NoError(json.Unmarshal([]byte(body), &reply))
	require.Equal([]string{"list", "object", "text"}, names(reply.Checks))
	require.JSONEq(`{"availableDiskBytes":12}`, string(found(require, reply.Checks, "object").Details))
	require.JSONEq(`"node is not a validator"`, string(found(require, reply.Checks, "text").Details))
	require.JSONEq(`["P","X","C"]`, string(found(require, reply.Checks, "list").Details))
}
