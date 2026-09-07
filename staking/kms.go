// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package staking includes a thin HTTP client to the external Lux KMS
// service (~/work/lux/kms, github.com/luxfi/kms). The KMS service speaks
// JSON over HTTP as its public contract — this file therefore stays on
// json/v2 by design. Do NOT migrate to ZAP: the wire format here is
// defined by the external service, not by us. Internal Lux paths that
// consume the keys MUST hash or copy the result into typed Go values
// before any internal codec.
//
// Not to be confused with Hanzo KMS (~/work/hanzo/kms) which serves
// Hanzoai apps — Lux blockchain node staking uses Lux KMS only.
package staking

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	// json/v2 is intentional: KMS HTTP RPC contract is JSON.
	"github.com/go-json-experiment/json"
)

// KMSConfig for staking key retrieval
type KMSConfig struct {
	Endpoint   string // KMS API endpoint (e.g., https://kms.dev.lux.network)
	SecretPath string // Path to the staking secret (e.g., /staking/devnet/node-0)
	AuthToken  string // Bearer token for KMS auth
}

// KMSStakingKeys holds the staking key material from KMS
type KMSStakingKeys struct {
	TLSKey    string `json:"tls_key"`    // PEM-encoded TLS private key
	TLSCert   string `json:"tls_cert"`   // PEM-encoded TLS certificate
	SignerKey string `json:"signer_key"` // BLS signer key (hex-encoded)
}

// FetchFromKMS retrieves staking keys from a KMS endpoint.
func FetchFromKMS(cfg KMSConfig) (*KMSStakingKeys, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("KMS endpoint must not be empty")
	}
	if cfg.SecretPath == "" {
		return nil, fmt.Errorf("KMS secret path must not be empty")
	}

	url := fmt.Sprintf("%s/api/v1/secrets%s", cfg.Endpoint, cfg.SecretPath)

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating KMS request: %w", err)
	}

	if cfg.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.AuthToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("KMS request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("KMS returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Secret struct {
			Value string `json:"secretValue"`
		} `json:"secret"`
	}
	if err := json.UnmarshalRead(resp.Body, &result); err != nil {
		return nil, fmt.Errorf("decoding KMS response: %w", err)
	}

	var keys KMSStakingKeys
	if err := json.Unmarshal([]byte(result.Secret.Value), &keys); err != nil {
		return nil, fmt.Errorf("parsing staking keys from KMS: %w", err)
	}

	return &keys, nil
}

// FetchFromKMSEnv reads KMS config from environment variables and fetches staking keys.
func FetchFromKMSEnv() (*KMSStakingKeys, error) {
	endpoint := os.Getenv("KMS_ENDPOINT")
	secretPath := os.Getenv("KMS_STAKING_PATH")
	authToken := os.Getenv("KMS_TOKEN")

	if endpoint == "" || secretPath == "" {
		return nil, fmt.Errorf("KMS_ENDPOINT and KMS_STAKING_PATH must be set")
	}

	return FetchFromKMS(KMSConfig{
		Endpoint:   endpoint,
		SecretPath: secretPath,
		AuthToken:  authToken,
	})
}
