// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap

import (
	"context"
	"testing"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/consensus/quasar"
	"github.com/stretchr/testify/require"
)

func TestParseDID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *DID
		wantErr bool
	}{
		{
			name:  "valid did:lux",
			input: "did:lux:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK",
			want: &DID{
				Method: DIDMethodLux,
				ID:     "z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK",
			},
			wantErr: false,
		},
		{
			name:  "valid did:key",
			input: "did:key:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK",
			want: &DID{
				Method: DIDMethodKey,
				ID:     "z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK",
			},
			wantErr: false,
		},
		{
			name:  "valid did:web",
			input: "did:web:example.com:users:alice",
			want: &DID{
				Method: DIDMethodWeb,
				ID:     "example.com:users:alice",
			},
			wantErr: false,
		},
		{
			name:    "invalid - no did prefix",
			input:   "lux:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK",
			wantErr: true,
		},
		{
			name:    "invalid - unknown method",
			input:   "did:unknown:abc123",
			wantErr: true,
		},
		{
			name:    "invalid - empty id",
			input:   "did:lux:",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDID(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want.Method, got.Method)
			require.Equal(t, tt.want.ID, got.ID)
		})
	}
}

func TestDIDString(t *testing.T) {
	did := &DID{Method: DIDMethodLux, ID: "z6MkTest"}
	require.Equal(t, "did:lux:z6MkTest", did.String())
}

func TestDIDValid(t *testing.T) {
	tests := []struct {
		name  string
		did   *DID
		valid bool
	}{
		{
			name:  "valid lux",
			did:   &DID{Method: DIDMethodLux, ID: "z6MkTest"},
			valid: true,
		},
		{
			name:  "valid key",
			did:   &DID{Method: DIDMethodKey, ID: "z6MkTest"},
			valid: true,
		},
		{
			name:  "valid web",
			did:   &DID{Method: DIDMethodWeb, ID: "example.com"},
			valid: true,
		},
		{
			name:  "nil did",
			did:   nil,
			valid: false,
		},
		{
			name:  "empty id",
			did:   &DID{Method: DIDMethodLux, ID: ""},
			valid: false,
		},
		{
			name:  "unknown method",
			did:   &DID{Method: "unknown", ID: "test"},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.valid, tt.did.Valid())
		})
	}
}

func TestDIDFromNodeID(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()
	did := DIDFromNodeID(nodeID)

	require.NotNil(t, did)
	require.Equal(t, DIDMethodLux, did.Method)
	require.True(t, did.Valid())
	require.Contains(t, did.String(), "did:lux:")
}

func TestDIDFromWeb(t *testing.T) {
	tests := []struct {
		name    string
		domain  string
		path    string
		wantID  string
		wantErr bool
	}{
		{
			name:    "domain only",
			domain:  "example.com",
			path:    "",
			wantID:  "example.com",
			wantErr: false,
		},
		{
			name:    "domain with path",
			domain:  "example.com",
			path:    "users/alice",
			wantID:  "example.com:users:alice",
			wantErr: false,
		},
		{
			name:    "empty domain",
			domain:  "",
			path:    "",
			wantErr: true,
		},
		{
			name:    "domain with slash",
			domain:  "example/com",
			path:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DIDFromWeb(tt.domain, tt.path)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, DIDMethodWeb, got.Method)
			require.Equal(t, tt.wantID, got.ID)
		})
	}
}

func TestGenerateDocument(t *testing.T) {
	did := &DID{Method: DIDMethodLux, ID: "z6MkTest"}
	doc, err := did.GenerateDocument()

	require.NoError(t, err)
	require.NotNil(t, doc)
	require.Equal(t, "did:lux:z6MkTest", doc.ID)
	require.Len(t, doc.Context, 2)
	require.Len(t, doc.VerificationMethod, 1)
	require.Len(t, doc.Authentication, 1)
	require.Len(t, doc.Service, 1)
	require.Equal(t, "ZapAgent", doc.Service[0].Type)
}

func TestBridgeNew(t *testing.T) {
	logger := log.Noop()
	config := DefaultBridgeConfig()
	bridge := NewBridge(logger, config)

	require.NotNil(t, bridge)
	require.Equal(t, 0.5, bridge.config.ConsensusThreshold)
}

func TestBridgeInitialize(t *testing.T) {
	logger := log.Noop()
	bridge := NewBridge(logger, DefaultBridgeConfig())

	// Test with nil coordinator
	err := bridge.Initialize(nil)
	require.ErrorIs(t, err, ErrNotInitialized)

	// Test with valid coordinator
	coordinator, err := quasar.NewRingtailCoordinator(logger, quasar.RingtailConfig{
		NumParties: 4,
		Threshold:  3,
		PartyIndex: 0,
	})
	require.NoError(t, err)

	err = bridge.Initialize(coordinator)
	require.NoError(t, err)
}

func TestBridgeRegisterValidator(t *testing.T) {
	logger := log.Noop()
	bridge := NewBridge(logger, DefaultBridgeConfig())

	nodeID := ids.GenerateTestNodeID()
	did := DIDFromNodeID(nodeID)

	// Register validator
	err := bridge.RegisterValidator(nodeID, did)
	require.NoError(t, err)

	// Get validator DID
	got, err := bridge.GetValidatorDID(nodeID)
	require.NoError(t, err)
	require.Equal(t, did.String(), got.String())

	// Unknown validator
	_, err = bridge.GetValidatorDID(ids.GenerateTestNodeID())
	require.ErrorIs(t, err, ErrValidatorNotFound)

	// Invalid DID
	err = bridge.RegisterValidator(nodeID, nil)
	require.ErrorIs(t, err, ErrInvalidDID)
}

func TestBridgeConsensusFlow(t *testing.T) {
	logger := log.Noop()
	bridge := NewBridge(logger, BridgeConfig{
		ConsensusThreshold: 0.5,
		MinResponses:       1,
		MinVotes:           2,
		EnablePQCrypto:     true,
	})

	// Initialize with coordinator
	coordinator, err := quasar.NewRingtailCoordinator(logger, quasar.RingtailConfig{
		NumParties: 4,
		Threshold:  3,
		PartyIndex: 0,
	})
	require.NoError(t, err)
	require.NoError(t, bridge.Initialize(coordinator))

	// Register validators
	submitter := ids.GenerateTestNodeID()
	responder := ids.GenerateTestNodeID()
	voter1 := ids.GenerateTestNodeID()
	voter2 := ids.GenerateTestNodeID()

	require.NoError(t, bridge.RegisterValidator(submitter, DIDFromNodeID(submitter)))
	require.NoError(t, bridge.RegisterValidator(responder, DIDFromNodeID(responder)))
	require.NoError(t, bridge.RegisterValidator(voter1, DIDFromNodeID(voter1)))
	require.NoError(t, bridge.RegisterValidator(voter2, DIDFromNodeID(voter2)))

	ctx := context.Background()

	// Submit query
	queryID := "query123"
	err = bridge.SubmitQuery(ctx, queryID, []byte("What is 2+2?"), submitter)
	require.NoError(t, err)

	// Submit response
	responseID := "response456"
	err = bridge.SubmitResponse(ctx, queryID, responseID, []byte("4"), responder)
	require.NoError(t, err)

	// Vote
	require.NoError(t, bridge.Vote(ctx, queryID, responseID, voter1))
	require.False(t, bridge.IsFinalized(queryID)) // Not enough votes yet

	require.NoError(t, bridge.Vote(ctx, queryID, responseID, voter2))
	require.True(t, bridge.IsFinalized(queryID)) // Now finalized

	// Get result
	result, err := bridge.GetResult(queryID)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, responseID, result.Response.ID)
	require.Equal(t, 2, result.Votes)
	require.Equal(t, 2, result.TotalVoters)
	require.Equal(t, 1.0, result.Confidence)
}

func TestBridgeDoubleVote(t *testing.T) {
	logger := log.Noop()
	bridge := NewBridge(logger, DefaultBridgeConfig())

	coordinator, _ := quasar.NewRingtailCoordinator(logger, quasar.RingtailConfig{
		NumParties: 2,
		Threshold:  1,
	})
	bridge.Initialize(coordinator)

	submitter := ids.GenerateTestNodeID()
	voter := ids.GenerateTestNodeID()

	bridge.RegisterValidator(submitter, DIDFromNodeID(submitter))
	bridge.RegisterValidator(voter, DIDFromNodeID(voter))

	ctx := context.Background()
	queryID := "q1"
	responseID := "r1"

	bridge.SubmitQuery(ctx, queryID, []byte("test"), submitter)
	bridge.SubmitResponse(ctx, queryID, responseID, []byte("answer"), submitter)

	// First vote succeeds
	require.NoError(t, bridge.Vote(ctx, queryID, responseID, voter))

	// Second vote fails
	err := bridge.Vote(ctx, queryID, responseID, voter)
	require.ErrorIs(t, err, ErrAlreadyVoted)
}

func TestBridgeStats(t *testing.T) {
	logger := log.Noop()
	bridge := NewBridge(logger, DefaultBridgeConfig())

	coordinator, _ := quasar.NewRingtailCoordinator(logger, quasar.RingtailConfig{
		NumParties: 4,
		Threshold:  3,
	})
	bridge.Initialize(coordinator)

	nodeID := ids.GenerateTestNodeID()
	bridge.RegisterValidator(nodeID, DIDFromNodeID(nodeID))

	stats := bridge.Stats()
	require.Equal(t, 1, stats.RegisteredValidators)
	require.Equal(t, 0, stats.ActiveQueries)
	require.Equal(t, 0, stats.FinalizedQueries)
	require.False(t, stats.QuasarInitialized) // Not initialized with validators
}

func TestNewQuery(t *testing.T) {
	did := &DID{Method: DIDMethodLux, ID: "z6MkTest"}
	query := NewQuery([]byte("What is 2+2?"), did)

	require.NotEmpty(t, query.ID)
	require.Equal(t, 64, len(query.ID)) // SHA-256 hex
	require.Equal(t, []byte("What is 2+2?"), query.Content)
	require.Equal(t, did, query.Submitter)
	require.Greater(t, query.Timestamp, int64(0))
}

func TestNewResponse(t *testing.T) {
	did := &DID{Method: DIDMethodLux, ID: "z6MkTest"}
	queryID := "0000000000000000000000000000000000000000000000000000000000000000"
	response := NewResponse(queryID, []byte("4"), did)

	require.NotEmpty(t, response.ID)
	require.Equal(t, 64, len(response.ID)) // SHA-256 hex
	require.Equal(t, queryID, response.QueryID)
	require.Equal(t, []byte("4"), response.Content)
	require.Equal(t, did, response.Responder)
}

func TestInMemoryStakeRegistry(t *testing.T) {
	registry := NewInMemoryStakeRegistry()
	did := &DID{Method: DIDMethodLux, ID: "z6MkTest"}

	// Initial stake is 0
	stake, err := registry.GetStake(did)
	require.NoError(t, err)
	require.Equal(t, uint64(0), stake)

	// Set stake
	require.NoError(t, registry.SetStake(did, 1000))

	// Get stake
	stake, err = registry.GetStake(did)
	require.NoError(t, err)
	require.Equal(t, uint64(1000), stake)

	// Total stake
	require.Equal(t, uint64(1000), registry.TotalStake())

	// Has sufficient stake
	has, err := registry.HasSufficientStake(did, 500)
	require.NoError(t, err)
	require.True(t, has)

	has, err = registry.HasSufficientStake(did, 2000)
	require.NoError(t, err)
	require.False(t, has)

	// Stake weight
	weight, err := registry.StakeWeight(did)
	require.NoError(t, err)
	require.Equal(t, 1.0, weight) // Only staker
}

func TestAgentMessageType(t *testing.T) {
	require.Equal(t, "Query", AgentMessageTypeQuery.String())
	require.Equal(t, "Response", AgentMessageTypeResponse.String())
	require.Equal(t, "Vote", AgentMessageTypeVote.String())
	require.Equal(t, "Finality", AgentMessageTypeFinality.String())
	require.Equal(t, "Unknown", AgentMessageType(255).String())
}

func TestBase58EncodeDecode(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"single byte", []byte{0x01}},
		{"multiple bytes", []byte{0x01, 0x02, 0x03, 0x04}},
		{"leading zeros", []byte{0x00, 0x00, 0x01, 0x02}},
		{"all zeros", []byte{0x00, 0x00, 0x00}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := base58Encode(tt.data)
			decoded, err := base58Decode(encoded)
			require.NoError(t, err)
			require.Equal(t, tt.data, decoded)
		})
	}
}

func TestBase58DecodeInvalidChar(t *testing.T) {
	_, err := base58Decode("0OIl") // Invalid chars
	require.Error(t, err)
}
