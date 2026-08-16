// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

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
		// One encoder for one wire type: apihealth.APIReply is defined with
		// encoding/json tags and carries a time.Duration per check, so it is
		// encoding/json that defines its representation. The POST (jsonrpc)
		// path already encodes it through that codec; encoding it here the
		// same way is what makes GET and POST agree. jsonv2 cannot encode
		// this type at all — time.Duration has no default representation
		// there — so a GET encoded through it degrades to the error fallback
		// below. encoding/json also replaces invalid UTF-8 (check Details
		// may embed raw chain-ID bytes) with U+FFFD rather than failing.
		//
		// Buffer first: a streaming encoder that errors part-way would leave
		// the client a torn body whose Content-Length matches the
		// truncation. Buffering makes the reply atomic.
		var buf bytes.Buffer
		err := json.NewEncoder(&buf).Encode(apihealth.APIReply{
			Checks:  checks,
			Healthy: healthy,
		})
		if err != nil {
			buf.Reset()
			fmt.Fprintf(&buf, `{"healthy":%t,"error":"health reply encode failed"}`, healthy)
		}
		_, _ = w.Write(buf.Bytes())
	})
}
