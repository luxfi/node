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
		reply := &apihealth.APIReply{}
		err := svc.Readiness(nil, &apihealth.APIArgs{}, reply)
		require.NoError(err)

		require.Len(reply.Checks, 1)
		require.Contains(reply.Checks, "check")
		require.Equal(notYetRunResult, reply.Checks["check"])
		require.False(reply.Healthy)
	}

	{
		reply := &apihealth.APIReply{}
		err := svc.Health(nil, &apihealth.APIArgs{}, reply)
		require.NoError(err)

		require.Len(reply.Checks, 1)
		require.Contains(reply.Checks, "check")
		require.Equal(notYetRunResult, reply.Checks["check"])
		require.False(reply.Healthy)
	}

	{
		reply := &apihealth.APIReply{}
		err := svc.Liveness(nil, &apihealth.APIArgs{}, reply)
		require.NoError(err)

		require.Len(reply.Checks, 1)
		require.Contains(reply.Checks, "check")
		require.Equal(notYetRunResult, reply.Checks["check"])
		require.False(reply.Healthy)
	}

	h.Start(context.Background(), checkFreq)
	defer h.Stop()

	awaitReadiness(t, h, true)
	awaitHealthy(t, h, true)
	awaitLiveness(t, h, true)

	{
		reply := &apihealth.APIReply{}
		err := svc.Readiness(nil, &apihealth.APIArgs{}, reply)
		require.NoError(err)

		result := reply.Checks["check"]
		require.Empty(result.Details)
		require.Nil(result.Error)
		require.Zero(result.ContiguousFailures)
		require.True(reply.Healthy)
	}

	{
		reply := &apihealth.APIReply{}
		err := svc.Health(nil, &apihealth.APIArgs{}, reply)
		require.NoError(err)

		result := reply.Checks["check"]
		require.Empty(result.Details)
		require.Nil(result.Error)
		require.Zero(result.ContiguousFailures)
		require.True(reply.Healthy)
	}

	{
		reply := &apihealth.APIReply{}
		err := svc.Liveness(nil, &apihealth.APIArgs{}, reply)
		require.NoError(err)

		result := reply.Checks["check"]
		require.Empty(result.Details)
		require.Nil(result.Error)
		require.Zero(result.ContiguousFailures)
		require.True(reply.Healthy)
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
		check    func(*Service, *apihealth.APIArgs, *apihealth.APIReply) error
		await    func(*testing.T, Reporter, bool)
	}

	tests := []testMethods{
		{
			name: "Readiness",
			register: func(h Health, s1 string, c Checker, s2 ...string) error {
				return h.RegisterReadinessCheck(s1, c, s2...)
			},
			check: func(s *Service, args *apihealth.APIArgs, reply *apihealth.APIReply) error {
				return s.Readiness(nil, args, reply)
			},
			await: awaitReadiness,
		},
		{
			name: "Health",
			register: func(h Health, s1 string, c Checker, s2 ...string) error {
				return h.RegisterHealthCheck(s1, c, s2...)
			},
			check: func(s *Service, args *apihealth.APIArgs, reply *apihealth.APIReply) error {
				return s.Health(nil, args, reply)
			},
			await: awaitHealthy,
		},
		{
			name: "Liveness",
			register: func(h Health, s1 string, c Checker, s2 ...string) error {
				return h.RegisterLivenessCheck(s1, c, s2...)
			},
			check: func(s *Service, args *apihealth.APIArgs, reply *apihealth.APIReply) error {
				return s.Liveness(nil, args, reply)
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
				reply := &apihealth.APIReply{}
				err := test.check(svc, &apihealth.APIArgs{}, reply)
				require.NoError(err)
				require.Len(reply.Checks, 4)
				require.Contains(reply.Checks, "check1")
				require.Contains(reply.Checks, "check2")
				require.Contains(reply.Checks, "check3")
				require.Contains(reply.Checks, "check4")
				require.Equal(notYetRunResult, reply.Checks["check1"])
				require.False(reply.Healthy)

				reply = &apihealth.APIReply{}
				err = test.check(svc, &apihealth.APIArgs{Tags: []string{netID1.String()}}, reply)
				require.NoError(err)
				require.Len(reply.Checks, 2)
				require.Contains(reply.Checks, "check2")
				require.Contains(reply.Checks, "check4")
				require.Equal(notYetRunResult, reply.Checks["check2"])
				require.False(reply.Healthy)
			}

			h.Start(context.Background(), checkFreq)

			test.await(t, h, true)

			{
				reply := &apihealth.APIReply{}
				err := test.check(svc, &apihealth.APIArgs{Tags: []string{netID1.String()}}, reply)
				require.NoError(err)
				require.Len(reply.Checks, 2)
				require.Contains(reply.Checks, "check2")
				require.Contains(reply.Checks, "check4")
				require.True(reply.Healthy)
			}

			// stop the health check
			h.Stop()

			{
				// now we'll add a new check which is unhealthy by default (notYetRunResult)
				require.NoError(test.register(h, "check5", check, netID1.String()))

				reply := &apihealth.APIReply{}
				err := test.check(svc, &apihealth.APIArgs{Tags: []string{netID1.String()}}, reply)
				require.NoError(err)
				require.Len(reply.Checks, 3)
				require.Contains(reply.Checks, "check2")
				require.Contains(reply.Checks, "check4")
				require.Contains(reply.Checks, "check5")
				require.Equal(notYetRunResult, reply.Checks["check5"])
				require.False(reply.Healthy)
			}
		})
	}
}
