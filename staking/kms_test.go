// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchFromKMS(t *testing.T) {
	wantKeys := KMSStakingKeys{
		TLSKey:    "-----BEGIN PRIVATE KEY-----\nfake-key\n-----END PRIVATE KEY-----",
		TLSCert:   "-----BEGIN CERTIFICATE-----\nfake-cert\n-----END CERTIFICATE-----",
		SignerKey: "deadbeef",
	}

	secretValue, err := json.Marshal(wantKeys)
	if err != nil {
		t.Fatal(err)
	}

	kmsResponse := map[string]interface{}{
		"secret": map[string]interface{}{
			"secretValue": string(secretValue),
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-token" {
			t.Errorf("expected Authorization header 'Bearer test-token', got %q", auth)
		}

		// Verify path
		if r.URL.Path != "/api/v1/secrets/staking/test/node-0" {
			t.Errorf("expected path '/api/v1/secrets/staking/test/node-0', got %q", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(kmsResponse)
	}))
	defer server.Close()

	keys, err := FetchFromKMS(KMSConfig{
		Endpoint:   server.URL,
		SecretPath: "/staking/test/node-0",
		AuthToken:  "test-token",
	})
	if err != nil {
		t.Fatalf("FetchFromKMS failed: %v", err)
	}

	if keys.TLSKey != wantKeys.TLSKey {
		t.Errorf("TLSKey = %q, want %q", keys.TLSKey, wantKeys.TLSKey)
	}
	if keys.TLSCert != wantKeys.TLSCert {
		t.Errorf("TLSCert = %q, want %q", keys.TLSCert, wantKeys.TLSCert)
	}
	if keys.SignerKey != wantKeys.SignerKey {
		t.Errorf("SignerKey = %q, want %q", keys.SignerKey, wantKeys.SignerKey)
	}
}

func TestFetchFromKMS_NoAuth(t *testing.T) {
	kmsResponse := map[string]interface{}{
		"secret": map[string]interface{}{
			"secretValue": `{"tls_key":"k","tls_cert":"c","signer_key":"s"}`,
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("expected no Authorization header, got %q", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(kmsResponse)
	}))
	defer server.Close()

	keys, err := FetchFromKMS(KMSConfig{
		Endpoint:   server.URL,
		SecretPath: "/staking/test/node-0",
	})
	if err != nil {
		t.Fatalf("FetchFromKMS failed: %v", err)
	}
	if keys.TLSKey != "k" {
		t.Errorf("TLSKey = %q, want %q", keys.TLSKey, "k")
	}
}

func TestFetchFromKMS_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("access denied"))
	}))
	defer server.Close()

	_, err := FetchFromKMS(KMSConfig{
		Endpoint:   server.URL,
		SecretPath: "/staking/test/node-0",
	})
	if err == nil {
		t.Fatal("expected error for 403 response, got nil")
	}
}

func TestFetchFromKMS_EmptyEndpoint(t *testing.T) {
	_, err := FetchFromKMS(KMSConfig{
		SecretPath: "/staking/test/node-0",
	})
	if err == nil {
		t.Fatal("expected error for empty endpoint, got nil")
	}
}

func TestFetchFromKMS_EmptySecretPath(t *testing.T) {
	_, err := FetchFromKMS(KMSConfig{
		Endpoint: "https://kms.example.com",
	})
	if err == nil {
		t.Fatal("expected error for empty secret path, got nil")
	}
}

func TestFetchFromKMS_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	_, err := FetchFromKMS(KMSConfig{
		Endpoint:   server.URL,
		SecretPath: "/staking/test/node-0",
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestFetchFromKMS_InvalidSecretValue(t *testing.T) {
	kmsResponse := map[string]interface{}{
		"secret": map[string]interface{}{
			"secretValue": "not-json-either",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(kmsResponse)
	}))
	defer server.Close()

	_, err := FetchFromKMS(KMSConfig{
		Endpoint:   server.URL,
		SecretPath: "/staking/test/node-0",
	})
	if err == nil {
		t.Fatal("expected error for invalid secret value JSON, got nil")
	}
}
