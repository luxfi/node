// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/luxfi/ids"
)

const (
	// MethodLux is the did:lux method for blockchain-anchored DIDs
	MethodLux = "lux"
	// MethodKey is the did:key method for self-certifying DIDs
	MethodKey = "key"
	// MethodWeb is the did:web method for DNS-based DIDs
	MethodWeb = "web"

	// MLDSAPublicKeySize is the expected size of ML-DSA-65 public keys
	MLDSAPublicKeySize = 1952

	// MultibaseBase58BTC is the multibase prefix for base58btc
	MultibaseBase58BTC = 'z'
)

// MulticodecMLDSA65 is the provisional multicodec prefix for ML-DSA-65
var MulticodecMLDSA65 = []byte{0x13, 0x09}

// DIDMethod represents a DID method identifier
type DIDMethod string

const (
	DIDMethodLux DIDMethod = MethodLux
	DIDMethodKey DIDMethod = MethodKey
	DIDMethodWeb DIDMethod = MethodWeb
)

// DID represents a W3C Decentralized Identifier
type DID struct {
	Method DIDMethod
	ID     string
}

// NewDID creates a DID from method and identifier
func NewDID(method DIDMethod, id string) *DID {
	return &DID{Method: method, ID: id}
}

// ParseDID parses a DID from a string in format "did:method:id"
func ParseDID(s string) (*DID, error) {
	if !strings.HasPrefix(s, "did:") {
		return nil, fmt.Errorf("%w: must start with 'did:'", ErrInvalidDID)
	}

	rest := s[4:] // Skip "did:"
	colonIndex := strings.Index(rest, ":")
	if colonIndex == -1 {
		return nil, fmt.Errorf("%w: expected 'did:method:id'", ErrInvalidDID)
	}

	methodStr := rest[:colonIndex]
	id := rest[colonIndex+1:]

	if id == "" {
		return nil, fmt.Errorf("%w: identifier cannot be empty", ErrInvalidDID)
	}

	var method DIDMethod
	switch methodStr {
	case MethodLux:
		method = DIDMethodLux
	case MethodKey:
		method = DIDMethodKey
	case MethodWeb:
		method = DIDMethodWeb
	default:
		return nil, fmt.Errorf("%w: unknown method '%s'", ErrInvalidDID, methodStr)
	}

	return &DID{Method: method, ID: id}, nil
}

// String returns the full DID URI
func (d *DID) String() string {
	if d == nil {
		return ""
	}
	return fmt.Sprintf("did:%s:%s", d.Method, d.ID)
}

// Valid checks if the DID is well-formed
func (d *DID) Valid() bool {
	if d == nil || d.ID == "" {
		return false
	}
	switch d.Method {
	case DIDMethodLux, DIDMethodKey, DIDMethodWeb:
		return true
	default:
		return false
	}
}

// DIDFromNodeID creates a did:lux from a Lux NodeID
func DIDFromNodeID(nodeID ids.NodeID) *DID {
	// Use multibase-encoded NodeID bytes
	encoded := base58Encode(nodeID[:])
	return &DID{
		Method: DIDMethodLux,
		ID:     string(MultibaseBase58BTC) + encoded,
	}
}

// DIDFromPublicKey creates a did:key from an ML-DSA-65 public key
func DIDFromPublicKey(publicKey []byte) (*DID, error) {
	if len(publicKey) != MLDSAPublicKeySize {
		return nil, fmt.Errorf("invalid ML-DSA public key size: expected %d, got %d",
			MLDSAPublicKeySize, len(publicKey))
	}

	// Prefix with multicodec for ML-DSA-65
	prefixed := make([]byte, len(MulticodecMLDSA65)+len(publicKey))
	copy(prefixed, MulticodecMLDSA65)
	copy(prefixed[len(MulticodecMLDSA65):], publicKey)

	// Encode with multibase (base58btc)
	encoded := base58Encode(prefixed)

	return &DID{
		Method: DIDMethodKey,
		ID:     string(MultibaseBase58BTC) + encoded,
	}, nil
}

// DIDFromWeb creates a did:web from a domain and optional path
func DIDFromWeb(domain string, path string) (*DID, error) {
	if domain == "" {
		return nil, fmt.Errorf("%w: domain cannot be empty", ErrInvalidDID)
	}
	if strings.ContainsAny(domain, "/:") {
		return nil, fmt.Errorf("%w: domain cannot contain '/' or ':'", ErrInvalidDID)
	}

	var id string
	if path != "" {
		// Replace '/' with ':' per did:web spec
		pathParts := strings.ReplaceAll(path, "/", ":")
		id = domain + ":" + pathParts
	} else {
		id = domain
	}

	return &DID{Method: DIDMethodWeb, ID: id}, nil
}

// ExtractKeyMaterial extracts raw key bytes from did:key or did:lux
func (d *DID) ExtractKeyMaterial() ([]byte, error) {
	if d == nil || d.ID == "" {
		return nil, fmt.Errorf("%w: empty identifier", ErrInvalidDID)
	}

	if d.ID[0] != MultibaseBase58BTC {
		return nil, fmt.Errorf("%w: unsupported multibase encoding", ErrInvalidDID)
	}

	// Decode base58btc (skip multibase prefix)
	decoded, err := base58Decode(d.ID[1:])
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidDID, err)
	}

	if len(decoded) < 2 {
		return nil, fmt.Errorf("%w: identifier too short", ErrInvalidDID)
	}

	// Skip multicodec prefix if it matches ML-DSA-65
	if len(decoded) >= 2 && decoded[0] == MulticodecMLDSA65[0] && decoded[1] == MulticodecMLDSA65[1] {
		return decoded[2:], nil
	}

	return decoded, nil
}

// Hash returns a 32-byte hash of the DID for indexing
func (d *DID) Hash() ids.ID {
	h := sha256.Sum256([]byte(d.String()))
	var id ids.ID
	copy(id[:], h[:])
	return id
}

// ToHex returns the DID hash as a hex string
func (d *DID) ToHex() string {
	id := d.Hash()
	return hex.EncodeToString(id[:])
}

// VerificationMethod represents a verification method in a DID Document
type VerificationMethod struct {
	ID                  string
	Type                string
	Controller          string
	PublicKeyMultibase  string
	BlockchainAccountID string
}

// Service represents a service endpoint in a DID Document
type Service struct {
	ID              string
	Type            string
	ServiceEndpoint string
}

// DIDDocument represents a W3C DID Document
type DIDDocument struct {
	Context              []string
	ID                   string
	Controller           string
	VerificationMethod   []VerificationMethod
	Authentication       []string
	AssertionMethod      []string
	KeyAgreement         []string
	CapabilityInvocation []string
	CapabilityDelegation []string
	Service              []Service
}

// GenerateDocument creates a DID Document for a DID
func (d *DID) GenerateDocument() (*DIDDocument, error) {
	if !d.Valid() {
		return nil, fmt.Errorf("%w: invalid DID", ErrInvalidDID)
	}

	uri := d.String()
	keyID := uri + "#keys-1"

	var vm VerificationMethod
	switch d.Method {
	case DIDMethodLux, DIDMethodKey:
		vm = VerificationMethod{
			ID:                 keyID,
			Type:               "JsonWebKey2020",
			Controller:         uri,
			PublicKeyMultibase: d.ID,
		}
		if d.Method == DIDMethodLux {
			// Add blockchain account ID
			keyMaterial, err := d.ExtractKeyMaterial()
			if err == nil && len(keyMaterial) >= 20 {
				vm.BlockchainAccountID = "lux:" + hex.EncodeToString(keyMaterial[:20])
			}
		}
	case DIDMethodWeb:
		vm = VerificationMethod{
			ID:         keyID,
			Type:       "JsonWebKey2020",
			Controller: uri,
		}
	}

	return &DIDDocument{
		Context: []string{
			"https://www.w3.org/ns/did/v1",
			"https://w3id.org/security/suites/jws-2020/v1",
		},
		ID:                   uri,
		VerificationMethod:   []VerificationMethod{vm},
		Authentication:       []string{keyID},
		AssertionMethod:      []string{keyID},
		CapabilityInvocation: []string{keyID},
		Service: []Service{
			{
				ID:              uri + "#zap-agent",
				Type:            "ZapAgent",
				ServiceEndpoint: "zap://" + d.ID,
			},
		},
	}, nil
}

// Base58 encoding/decoding (Bitcoin alphabet)
const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func base58Encode(data []byte) string {
	// Convert to big integer
	result := make([]byte, 0, len(data)*2)
	for _, b := range data {
		carry := int(b)
		for i := 0; i < len(result); i++ {
			carry += int(result[i]) << 8
			result[i] = byte(carry % 58)
			carry /= 58
		}
		for carry > 0 {
			result = append(result, byte(carry%58))
			carry /= 58
		}
	}

	// Handle leading zeros
	for _, b := range data {
		if b != 0 {
			break
		}
		result = append(result, 0)
	}

	// Reverse and convert to alphabet
	output := make([]byte, len(result))
	for i := range result {
		output[len(result)-1-i] = base58Alphabet[result[i]]
	}

	return string(output)
}

func base58Decode(s string) ([]byte, error) {
	result := make([]byte, 0, len(s))
	for _, c := range s {
		index := strings.IndexRune(base58Alphabet, c)
		if index == -1 {
			return nil, fmt.Errorf("invalid base58 character: %c", c)
		}

		carry := index
		for i := 0; i < len(result); i++ {
			carry += int(result[i]) * 58
			result[i] = byte(carry)
			carry >>= 8
		}
		for carry > 0 {
			result = append(result, byte(carry))
			carry >>= 8
		}
	}

	// Handle leading ones
	for _, c := range s {
		if c != rune(base58Alphabet[0]) {
			break
		}
		result = append(result, 0)
	}

	// Reverse
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	return result, nil
}
