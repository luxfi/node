// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/log"
	"github.com/luxfi/metric"
	"github.com/luxfi/node/service/health"
)

// TestProbePaths pins the addresses a Kubernetes probe must use, and the codes
// it reads there while a chain is still bootstrapping.
//
// The three probes ask three different questions and a node answers each on its
// own path. Pointing all three at one address is what turns a node that is
// merely syncing into a node that is repeatedly killed for it, so the paths
// below are the contract k8s/base/statefulset.yaml is written against.
func TestProbePaths(t *testing.T) {
	reporter, err := health.New(log.NewNoOpLogger(), metric.NewRegistry())
	require.NoError(t, err)

	// A bootstrapping chain, registered exactly as chains/manager.go does it:
	// the same check on readiness AND health, and nothing on liveness.
	syncing := health.CheckerFunc(func(context.Context) (interface{}, error) {
		return nil, errors.New("not bootstrapped")
	})
	require.NoError(t, reporter.RegisterReadinessCheck("bootstrapped", syncing, health.ApplicationTag))
	require.NoError(t, reporter.RegisterHealthCheck("bootstrapped", syncing, health.ApplicationTag))
	reporter.Start(context.Background(), time.Millisecond)
	defer reporter.Stop()
	time.Sleep(50 * time.Millisecond) // let the workers record a result

	handler, err := Mount(health.NewService(log.NewNoOpLogger(), reporter).Ops())
	require.NoError(t, err)

	r := newRouter()
	require.NoError(t, r.AddRouter(baseURL+"/health", Ops, handler))

	get := func(path string) int {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec.Code
	}

	for path, want := range map[string]int{
		// Liveness is the probe that can kill the pod. A syncing node is alive.
		baseURL + "/health/ops/liveness": http.StatusOK,
		// Readiness keeps a node that cannot answer yet out of the Service.
		baseURL + "/health/ops/readiness": http.StatusServiceUnavailable,
		baseURL + "/health/ops/health":    http.StatusServiceUnavailable,
	} {
		require.Equal(t, want, get(path), "GET %s", path)
	}

	// The pre-cutover address. It is gone, and a probe aimed here reads 404 —
	// which fails a probe exactly as a 503 does.
	require.Equal(t, http.StatusNotFound, get(baseURL+"/health"), "the old probe path must not answer")
}

// TestHealthzFollowsLiveness proves /healthz is an alias and not an opinion.
//
// It used to look up an endpoint the ops cutover had renamed, miss, and fall
// through to an unconditional 200 — so the one address a platform probes could
// never report a node that needed restarting.
func TestHealthzFollowsLiveness(t *testing.T) {
	reporter, err := health.New(log.NewNoOpLogger(), metric.NewRegistry())
	require.NoError(t, err)

	dead := health.CheckerFunc(func(context.Context) (interface{}, error) {
		return nil, errors.New("wedged")
	})
	require.NoError(t, reporter.RegisterLivenessCheck("wedged", dead, health.ApplicationTag))
	reporter.Start(context.Background(), time.Millisecond)
	defer reporter.Stop()
	time.Sleep(50 * time.Millisecond)

	handler, err := Mount(health.NewService(log.NewNoOpLogger(), reporter).Ops())
	require.NoError(t, err)
	r := newRouter()
	require.NoError(t, r.AddRouter(baseURL+"/health", Ops, handler))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code,
		"/healthz must report the liveness verdict, not a fixed 200")
}
