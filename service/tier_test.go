// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Which tier each of this node's own services answers in.
//
// The node's rule reads an operation's METHOD and nothing else
// (server/http/authorize.go): GET and HEAD answer anyone, everything else
// answers the operator. So the verb chosen at registration IS the access
// decision, and getting it wrong looks exactly like a working conversion —
// a read registered as a POST still serves every test that calls it in
// process, and answers 403 to the world.
//
// info, health and security are PUBLIC. A node tells anyone what it is running,
// whether it is up, and what security it enforces; a wallet checks the last of
// those BEFORE it trusts the chain, so a 403 there is a chain nobody can verify.
// Every one of their operations is asserted open, by walking the registry rather
// than by listing them — a list is a second place to remember.
//
// admin is the operator's. Its reads are open for the same reason info's are:
// what may be DISCLOSED is governed by the type, not by the rule. Its changes
// are not, and db/value is a read that is not open either — its input names any
// key in the node's database, so what it discloses is chosen by the caller and
// no type can mark it.
//
// This test lives beside the services rather than inside one because it is one
// question asked of all four, and the answer is a property of the set.
package service_test

import (
	"testing"

	"github.com/luxfi/log"
	"github.com/stretchr/testify/require"
	"github.com/zap-proto/zip"

	server "github.com/luxfi/node/server/http"
	"github.com/luxfi/node/service/admin"
	"github.com/luxfi/node/service/health"
	"github.com/luxfi/node/service/info"
	"github.com/luxfi/node/service/security"
)

// public are the services a node answers to anyone.
func public() map[string]*zip.App {
	return map[string]*zip.App{
		"info":     info.New(info.Parameters{}, log.Noop(), nil, nil, nil, nil, nil).Ops(),
		"health":   health.NewService(log.Noop(), nil).Ops(),
		"security": security.New(log.Noop(), nil).Ops(),
	}
}

// TestEveryPublicOpAnswersWhoeverAsks is the mutation this file exists for:
// change any GET below to a Post and it fails here, rather than on mainnet.
func TestEveryPublicOpAnswersWhoeverAsks(t *testing.T) {
	for name, app := range public() {
		t.Run(name, func(t *testing.T) {
			t.Cleanup(func() { _ = app.Shutdown() })
			ops := app.Routes()
			require.NotEmpty(t, ops, "the app registered no operations at all")
			for _, route := range ops {
				if route.Op == "" {
					continue // zip's own control routes: the document, the door
				}
				require.True(t,
					server.Open(zip.Op{Method: route.Method, Path: route.Pattern}),
					"%s %s is in the operator tier; %s is public", route.Method, route.Pattern, name)
			}
		})
	}
}

// TestAdminChangesAnswerToTheOperator is the other half. A change registered as
// a GET would answer whoever asks, which is the same defect in the other
// direction and the more dangerous one.
func TestAdminChangesAnswerToTheOperator(t *testing.T) {
	app := admin.New(admin.Config{Log: log.Noop()}).Ops()
	t.Cleanup(func() { _ = app.Shutdown() })

	// The reads admin answers to anyone, named because they are the exception
	// and an exception should be written down. Everything else changes the node
	// — or, for db/value, discloses whatever key the caller names.
	open := map[string]bool{
		"GET /chain/aliases":  true,
		"GET /log/level":      true,
		"GET /config":         true,
		"GET /plugins":        true,
		"GET /chains/tracked": true,
	}

	seen := map[string]bool{}
	for _, route := range app.Routes() {
		if route.Op == "" {
			continue
		}
		at := route.Method + " " + route.Pattern
		seen[at] = true
		require.Equal(t, open[at],
			server.Open(zip.Op{Method: route.Method, Path: route.Pattern}),
			"%s is in the wrong tier", at)
	}
	for at := range open {
		require.True(t, seen[at], "%s is named open but is not registered", at)
	}
}

// TestNoTwoOpsShareAnAddress is a constraint of the FRAMEWORK, not of routing.
//
// A mount makes two apps' addresses distinct — /v1/info/ops/vms is not
// /v1/admin/ops/vms — but zip files an op's prose in a package-level map keyed
// by "METHOD path" alone (zip doc.go:54). Two apps in one process that register
// the same path therefore share ONE doc entry, and the one that initialises
// last wins: info's /vms and admin's /vms published each other's descriptions,
// in the OpenAPI document and in the MCP tool an agent reads.
//
// Nothing fails when that happens. The route works, the schema is right, and
// only the sentence is another operation's — which is exactly the kind of wrong
// answer nobody notices. So the paths are kept distinct, and this is what says
// so. It sees this node's own four services; a chain app registering one of
// these paths would collide the same way.
func TestNoTwoOpsShareAnAddress(t *testing.T) {
	at := map[string]string{}
	apps := public()
	apps["admin"] = admin.New(admin.Config{Log: log.Noop()}).Ops()
	for name, app := range apps {
		t.Cleanup(func() { _ = app.Shutdown() })
		for _, route := range app.Routes() {
			if route.Op == "" {
				continue
			}
			key := route.Method + " " + route.Pattern
			if first, taken := at[key]; taken {
				t.Errorf("%s is registered by both %s and %s; they would publish each other's prose", key, first, name)
			}
			at[key] = name
		}
	}
}
