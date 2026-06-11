// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"bytes"
	"fmt"
	"net/http"

	"github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"github.com/gorilla/rpc/v2"

	apihealth "github.com/luxfi/api/health"
	"github.com/luxfi/log"

	avajson "github.com/luxfi/node/utils/json"
)

// NewGetAndPostHandler returns a health handler that supports GET and jsonrpc
// POST requests.
func NewGetAndPostHandler(log log.Logger, reporter Reporter) (http.Handler, error) {
	newServer := rpc.NewServer()
	codec := avajson.NewCodec()
	newServer.RegisterCodec(codec, "application/json")
	newServer.RegisterCodec(codec, "application/json;charset=UTF-8")

	getHandler := NewGetHandler(reporter.Health)

	// If a GET request is sent, we respond with a 200 if the node is healthy or
	// a 503 if the node isn't healthy.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			newServer.ServeHTTP(w, r)
			return
		}

		getHandler.ServeHTTP(w, r)
	})

	err := newServer.RegisterService(NewService(log, reporter), "health")
	return handler, err
}

// NewGetHandler return a health handler that supports GET requests reporting
// the result of the provided [reporter].
func NewGetHandler(reporter func(tags ...string) (map[string]apihealth.Result, bool)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Make sure the content type is set before writing the header.
		w.Header().Set("Content-Type", "application/json")

		tags := r.URL.Query()["tag"]
		checks, healthy := reporter(tags...)
		if !healthy {
			// If a health check has failed, we should return a 503.
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		// Buffer the reply — a streaming encoder that errors part-way
		// (jsonv2 rejects invalid UTF-8, and check Details may embed raw
		// chain-ID bytes) leaves the client a torn body whose
		// Content-Length matches the truncation. Buffering makes the
		// reply atomic; AllowInvalidUTF8 turns binary detail bytes into
		// replacement runes instead of an encode error.
		var buf bytes.Buffer
		err := json.MarshalWrite(&buf, apihealth.APIReply{
			Checks:  checks,
			Healthy: healthy,
		}, jsontext.AllowInvalidUTF8(true))
		if err != nil {
			buf.Reset()
			fmt.Fprintf(&buf, `{"healthy":%t,"error":"health reply encode failed"}`, healthy)
		}
		_, _ = w.Write(buf.Bytes())
	})
}
