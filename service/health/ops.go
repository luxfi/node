// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Whether this node is up, ready, and well.
//
// Three questions, three answers, one shape. Each is a GET — a health check
// changes nothing and a node tells anyone whether it is alive — and each answers
// 200 when every check passed and 503 when one did not.
//
// THE STATUS RIDES THE ANSWER. [apihealth.APIReply] states its own code, so the
// 503 a probe reads and the `healthy: false` a person reads are the same fact
// computed once. Declaring both codes is what puts them in the OpenAPI document,
// so a generated client and a Kubernetes probe expect what this actually sends.

package health

import (
	"context"
	"slices"
	"strings"

	apihealth "github.com/luxfi/api/health"
	"github.com/luxfi/log"
	"github.com/zap-proto/zip"
)

//go:generate go run github.com/zap-proto/zip/cmd/zipdoc

// probe is the two codes every operation here answers with.
var probe = zip.WithStatus(200, 503)

// Ops is this service's typed operations. The paths are relative to where the
// app is mounted, which the node decides — a service does not name its own
// address.
func (s *Service) Ops() *zip.App {
	app := zip.New(zip.Config{
		AppName:               "health",
		Logger:                s.log,
		DisableStartupMessage: true,
		OpenAPI: zip.OpenAPIConfig{
			Title:       "Lux node health",
			Description: "Whether a Lux node has finished starting, is answering, and is well enough to keep.",
		},
	})
	zip.Get(app, "/readiness", s.readiness, probe)
	zip.Get(app, "/health", s.health, probe)
	zip.Get(app, "/liveness", s.liveness, probe)
	return app
}

// Readiness is whether this node has finished starting: every check that has to
// pass once has passed. Answers 503 until it has.
//
// Example: {"tags": []}
// Response: {"checks": {"bootstrapped": {"message": {"P": true}, "timestamp": "2026-08-29T00:00:00Z", "duration": 100000}}, "healthy": true}
func (s *Service) readiness(_ context.Context, in *apihealth.APIArgs) (*apihealth.APIReply, error) {
	s.log.Debug("API called",
		log.UserString("service", "health"),
		log.UserString("method", "readiness"),
		log.Reflect("tags", in.Tags),
	)
	return reply(s.reporter.Readiness(in.Tags...))
}

// Health is whether this node is well: every check is passing now. Answers 503
// when one is not.
//
// Example: {"tags": []}
// Response: {"checks": {"database": {"timestamp": "2026-08-29T00:00:00Z", "duration": 100000}}, "healthy": true}
func (s *Service) health(_ context.Context, in *apihealth.APIArgs) (*apihealth.APIReply, error) {
	s.log.Debug("API called",
		log.UserString("service", "health"),
		log.UserString("method", "health"),
		log.Reflect("tags", in.Tags),
	)
	return reply(s.reporter.Health(in.Tags...))
}

// Liveness is whether this node is worth keeping: a 503 here is a node that
// needs restarting rather than one that is merely unwell.
//
// Example: {"tags": []}
// Response: {"checks": {}, "healthy": true}
func (s *Service) liveness(_ context.Context, in *apihealth.APIArgs) (*apihealth.APIReply, error) {
	s.log.Debug("API called",
		log.UserString("service", "health"),
		log.UserString("method", "liveness"),
		log.Reflect("tags", in.Tags),
	)
	return reply(s.reporter.Liveness(in.Tags...))
}

// reply is what a reporter answered, as the wire carries it: the checks in name
// order, because a map has no order and two reads of one answer should not
// differ.
func reply(checks map[string]apihealth.Result, healthy bool) (*apihealth.APIReply, error) {
	out := &apihealth.APIReply{
		Checks:  make(apihealth.Checks, 0, len(checks)),
		Healthy: healthy,
	}
	for name, result := range checks {
		out.Checks = append(out.Checks, apihealth.Check{Name: name, Result: result})
	}
	slices.SortFunc(out.Checks, func(x, y apihealth.Check) int { return strings.Compare(x.Name, y.Name) })
	return out, nil
}
