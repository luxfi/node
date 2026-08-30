// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"context"
	"testing"

	apihealth "github.com/luxfi/api/health"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/metric"
	"github.com/stretchr/testify/require"
)

func TestServiceResponses(t *testing.T) {
	require := require.New(t)

	check := CheckerFunc(func(context.Context) (interface{}, error) {
		return "", nil
	})

	h, err := New(log.NewNoOpLogger(), metric.NewRegistry())
	require.NoError(err)

	svc := NewService(log.NewNoOpLogger(), h)

	require.NoError(h.RegisterReadinessCheck("check", check))
	require.NoError(h.RegisterHealthCheck("check", check))
	require.NoError(h.RegisterLivenessCheck("check", check))

	{
		reply, err := svc.readiness(t.Context(), &apihealth.APIArgs{})
		require.NoError(err)

		require.Equal(apihealth.Checks{{Name: "check", Result: notYetRunResult}}, reply.Checks)
		require.False(reply.Healthy)
		require.Equal(503, reply.StatusCode())
	}

	{
		reply, err := svc.health(t.Context(), &apihealth.APIArgs{})
		require.NoError(err)

		require.Equal(apihealth.Checks{{Name: "check", Result: notYetRunResult}}, reply.Checks)
		require.False(reply.Healthy)
		require.Equal(503, reply.StatusCode())
	}

	{
		reply, err := svc.liveness(t.Context(), &apihealth.APIArgs{})
		require.NoError(err)

		require.Equal(apihealth.Checks{{Name: "check", Result: notYetRunResult}}, reply.Checks)
		require.False(reply.Healthy)
		require.Equal(503, reply.StatusCode())
	}

	h.Start(context.Background(), checkFreq)
	defer h.Stop()

	awaitReadiness(t, h, true)
	awaitHealthy(t, h, true)
	awaitLiveness(t, h, true)

	{
		reply, err := svc.readiness(t.Context(), &apihealth.APIArgs{})
		require.NoError(err)

		check, ok := reply.Checks.Find("check")
		require.True(ok)
		require.JSONEq(`""`, string(check.Result.Details))
		require.Nil(check.Result.Error)
		require.Zero(check.Result.ContiguousFailures)
		require.True(reply.Healthy)
		require.Equal(200, reply.StatusCode())
	}

	{
		reply, err := svc.health(t.Context(), &apihealth.APIArgs{})
		require.NoError(err)

		check, ok := reply.Checks.Find("check")
		require.True(ok)
		require.JSONEq(`""`, string(check.Result.Details))
		require.Nil(check.Result.Error)
		require.Zero(check.Result.ContiguousFailures)
		require.True(reply.Healthy)
		require.Equal(200, reply.StatusCode())
	}

	{
		reply, err := svc.liveness(t.Context(), &apihealth.APIArgs{})
		require.NoError(err)

		check, ok := reply.Checks.Find("check")
		require.True(ok)
		require.JSONEq(`""`, string(check.Result.Details))
		require.Nil(check.Result.Error)
		require.Zero(check.Result.ContiguousFailures)
		require.True(reply.Healthy)
		require.Equal(200, reply.StatusCode())
	}
}

func TestServiceTagResponse(t *testing.T) {
	check := CheckerFunc(func(context.Context) (interface{}, error) {
		return "", nil
	})

	netID1 := ids.GenerateTestID()
	netID2 := ids.GenerateTestID()

	type testMethods struct {
		name     string
		register func(Health, string, Checker, ...string) error
		check    func(*Service, *apihealth.APIArgs) (*apihealth.APIReply, error)
		await    func(*testing.T, Reporter, bool)
	}

	tests := []testMethods{
		{
			name: "Readiness",
			register: func(h Health, s1 string, c Checker, s2 ...string) error {
				return h.RegisterReadinessCheck(s1, c, s2...)
			},
			check: func(s *Service, args *apihealth.APIArgs) (*apihealth.APIReply, error) {
				return s.readiness(context.Background(), args)
			},
			await: awaitReadiness,
		},
		{
			name: "Health",
			register: func(h Health, s1 string, c Checker, s2 ...string) error {
				return h.RegisterHealthCheck(s1, c, s2...)
			},
			check: func(s *Service, args *apihealth.APIArgs) (*apihealth.APIReply, error) {
				return s.health(context.Background(), args)
			},
			await: awaitHealthy,
		},
		{
			name: "Liveness",
			register: func(h Health, s1 string, c Checker, s2 ...string) error {
				return h.RegisterLivenessCheck(s1, c, s2...)
			},
			check: func(s *Service, args *apihealth.APIArgs) (*apihealth.APIReply, error) {
				return s.liveness(context.Background(), args)
			},
			await: awaitLiveness,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			h, err := New(log.NewNoOpLogger(), metric.NewRegistry())
			require.NoError(err)
			require.NoError(test.register(h, "check1", check))
			require.NoError(test.register(h, "check2", check, netID1.String()))
			require.NoError(test.register(h, "check3", check, netID2.String()))
			require.NoError(test.register(h, "check4", check, netID1.String(), netID2.String()))

			svc := NewService(log.NewNoOpLogger(), h)

			// default checks
			{
				reply, err := test.check(svc, &apihealth.APIArgs{})
				require.NoError(err)
				require.Equal([]string{"check1", "check2", "check3", "check4"}, names(reply.Checks))
				require.Equal(notYetRunResult, found(require, reply.Checks, "check1"))
				require.False(reply.Healthy)

				reply, err = test.check(svc, &apihealth.APIArgs{Tags: []string{netID1.String()}})
				require.NoError(err)
				require.Equal([]string{"check2", "check4"}, names(reply.Checks))
				require.Equal(notYetRunResult, found(require, reply.Checks, "check2"))
				require.False(reply.Healthy)
			}

			h.Start(context.Background(), checkFreq)

			test.await(t, h, true)

			{
				reply, err := test.check(svc, &apihealth.APIArgs{Tags: []string{netID1.String()}})
				require.NoError(err)
				require.Equal([]string{"check2", "check4"}, names(reply.Checks))
				require.True(reply.Healthy)
			}

			// stop the health check
			h.Stop()

			{
				// now we'll add a new check which is unhealthy by default (notYetRunResult)
				require.NoError(test.register(h, "check5", check, netID1.String()))

				reply, err := test.check(svc, &apihealth.APIArgs{Tags: []string{netID1.String()}})
				require.NoError(err)
				require.Equal([]string{"check2", "check4", "check5"}, names(reply.Checks))
				require.Equal(notYetRunResult, found(require, reply.Checks, "check5"))
				require.False(reply.Healthy)
			}
		})
	}
}

// names are the checks a reply carries, in the order it carries them — which is
// the point of the list: a map answered the same question two ways.
func names(checks apihealth.Checks) []string {
	out := make([]string, len(checks))
	for i, c := range checks {
		out[i] = c.Name
	}
	return out
}

func found(require *require.Assertions, checks apihealth.Checks, name string) apihealth.Result {
	check, ok := checks.Find(name)
	require.True(ok, "reply carries no check named %q", name)
	return check.Result
}
