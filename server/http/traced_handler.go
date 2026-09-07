// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"net/http"

	"github.com/luxfi/node/trace"
)

var _ http.Handler = (*tracedHandler)(nil)

type tracedHandler struct {
	h            http.Handler
	serveHTTPTag string
	tracer       trace.Tracer
}

func TraceHandler(h http.Handler, name string, tracer trace.Tracer) http.Handler {
	return &tracedHandler{
		h:            h,
		serveHTTPTag: name + ".ServeHTTP",
		tracer:       tracer,
	}
}

func (h *tracedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ctx, span := h.tracer.Start(ctx, h.serveHTTPTag, trace.WithAttributes(
		trace.String("method", r.Method),
		trace.String("url", r.URL.Redacted()),
		trace.String("proto", r.Proto),
		trace.String("host", r.Host),
		trace.String("remoteAddr", r.RemoteAddr),
		trace.String("requestURI", r.RequestURI),
	))
	defer span.End()

	r = r.WithContext(ctx)
	h.h.ServeHTTP(w, r)
}
