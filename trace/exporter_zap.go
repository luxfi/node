//go:build !grpc

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package trace

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

const tracerProviderExportCreationTimeout = 5 * time.Second

type ExporterConfig struct {
	Type ExporterType `json:"type"`

	// Endpoint to send traces to. If empty, the default endpoint will be used.
	Endpoint string `json:"endpoint"`

	// Headers to send with traces
	Headers map[string]string `json:"headers"`

	// If true, don't use TLS
	Insecure bool `json:"insecure"`
}

func newExporter(config ExporterConfig) (sdktrace.SpanExporter, error) {
	var client otlptrace.Client
	switch config.Type {
	case GRPC:
		// gRPC not available in default build - use ZAP or HTTP instead
		return nil, fmt.Errorf("gRPC exporter requires -tags=grpc build flag, use 'zap' or 'http' instead")
	case HTTP, ZAP:
		// Both HTTP and ZAP use direct HTTP/protobuf without gRPC dependency
		client = &httpTraceClient{
			endpoint: config.Endpoint,
			headers:  config.Headers,
			insecure: config.Insecure,
			timeout:  tracerExportTimeout,
		}
	default:
		return nil, errUnknownExporterType
	}

	ctx, cancel := context.WithTimeout(context.Background(), tracerProviderExportCreationTimeout)
	defer cancel()
	return otlptrace.New(ctx, client)
}

// marshalExportRequest constructs an OTLP ExportTraceServiceRequest protobuf
// without importing the collector package (which has gRPC dependencies).
// The message format is: repeated ResourceSpans resource_spans = 1;
func marshalExportRequest(spans []*tracepb.ResourceSpans) ([]byte, error) {
	var buf []byte
	for _, span := range spans {
		data, err := proto.Marshal(span)
		if err != nil {
			return nil, err
		}
		// Field 1, wire type 2 (length-delimited)
		buf = protowire.AppendTag(buf, 1, protowire.BytesType)
		buf = protowire.AppendBytes(buf, data)
	}
	return buf, nil
}

// httpTraceClient implements otlptrace.Client using direct HTTP/protobuf
// without gRPC overhead. This is OTLP/HTTP with our own lightweight implementation.
type httpTraceClient struct {
	endpoint string
	headers  map[string]string
	insecure bool
	timeout  time.Duration

	mu     sync.Mutex
	client *http.Client
}

func (c *httpTraceClient) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:        10,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	if !c.insecure {
		transport.TLSClientConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	c.client = &http.Client{
		Transport: transport,
		Timeout:   c.timeout,
	}

	return nil
}

func (c *httpTraceClient) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		c.client.CloseIdleConnections()
		c.client = nil
	}
	return nil
}

func (c *httpTraceClient) UploadTraces(ctx context.Context, protoSpans []*tracepb.ResourceSpans) error {
	c.mu.Lock()
	client := c.client
	c.mu.Unlock()

	if client == nil {
		return fmt.Errorf("client not started")
	}

	// Build OTLP request manually (avoids gRPC-dependent collector package)
	data, err := marshalExportRequest(protoSpans)
	if err != nil {
		return fmt.Errorf("marshal traces: %w", err)
	}

	// Build endpoint URL
	endpoint := c.endpoint
	if endpoint == "" {
		endpoint = "localhost:4318"
	}

	scheme := "https"
	if c.insecure {
		scheme = "http"
	}

	u := &url.URL{
		Scheme: scheme,
		Host:   endpoint,
		Path:   "/v1/traces",
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	for k, v := range c.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send traces: %w", err)
	}
	defer resp.Body.Close()

	// Drain body to allow connection reuse
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}
