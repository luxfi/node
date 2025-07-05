// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/luxfi/node/ids"
)

// AttestationHandler handles attestation-related HTTP requests
type AttestationHandler struct {
	vm *VM
}

// NewAttestationHandler creates a new attestation handler
func NewAttestationHandler(vm *VM) http.Handler {
	handler := &AttestationHandler{vm: vm}
	
	mux := http.NewServeMux()
	mux.HandleFunc("/submit", handler.handleSubmit)
	mux.HandleFunc("/get", handler.handleGet)
	mux.HandleFunc("/list", handler.handleList)
	mux.HandleFunc("/status", handler.handleStatus)
	
	return mux
}

// handleSubmit handles attestation submission
func (h *AttestationHandler) handleSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	var req AttestationSubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	
	// Create attestation from request
	att := &Attestation{
		Type:       req.Type,
		SourceID:   req.SourceID,
		Data:       req.Data,
		Timestamp:  time.Now().Unix(),
		Signatures: req.Signatures,
		SignerIDs:  req.SignerIDs,
		Proof:      req.Proof,
		Metadata:   req.Metadata,
	}
	
	// Submit attestation
	if err := h.vm.SubmitAttestation(att); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	
	// Return response
	resp := AttestationSubmitResponse{
		ID:      att.ID.String(),
		Success: true,
		Message: "Attestation submitted successfully",
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleGet handles getting an attestation by ID
func (h *AttestationHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Error(w, "ID parameter required", http.StatusBadRequest)
		return
	}
	
	id, err := ids.FromString(idStr)
	if err != nil {
		http.Error(w, "Invalid ID format", http.StatusBadRequest)
		return
	}
	
	att, err := h.vm.attestationDB.GetAttestation(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(att)
}

// handleList handles listing attestations
func (h *AttestationHandler) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	limitStr := r.URL.Query().Get("limit")
	limit := 100
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}
	
	attestations, err := h.vm.attestationDB.GetPendingAttestations(limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	resp := AttestationListResponse{
		Attestations: attestations,
		Count:        len(attestations),
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleStatus handles VM status
func (h *AttestationHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	health, err := h.vm.HealthCheck(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}

// OracleHandler handles oracle-related HTTP requests
type OracleHandler struct {
	vm *VM
}

// NewOracleHandler creates a new oracle handler
func NewOracleHandler(vm *VM) http.Handler {
	handler := &OracleHandler{vm: vm}
	
	mux := http.NewServeMux()
	mux.HandleFunc("/register", handler.handleRegister)
	mux.HandleFunc("/get", handler.handleGet)
	mux.HandleFunc("/list", handler.handleList)
	mux.HandleFunc("/feeds", handler.handleFeeds)
	
	return mux
}

// handleRegister handles oracle registration
func (h *OracleHandler) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	if h.vm.oracleRegistry == nil {
		http.Error(w, "Oracle registry not enabled", http.StatusNotImplemented)
		return
	}
	
	var oracle OracleInfo
	if err := json.NewDecoder(r.Body).Decode(&oracle); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	
	if err := h.vm.oracleRegistry.RegisterOracle(&oracle); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	
	resp := OracleRegisterResponse{
		Success: true,
		Message: "Oracle registered successfully",
		ID:      oracle.ID,
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleGet handles getting an oracle by ID
func (h *OracleHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	if h.vm.oracleRegistry == nil {
		http.Error(w, "Oracle registry not enabled", http.StatusNotImplemented)
		return
	}
	
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "ID parameter required", http.StatusBadRequest)
		return
	}
	
	oracle, err := h.vm.oracleRegistry.GetOracle(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(oracle)
}

// handleList handles listing all oracles
func (h *OracleHandler) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	if h.vm.oracleRegistry == nil {
		http.Error(w, "Oracle registry not enabled", http.StatusNotImplemented)
		return
	}
	
	oracles := h.vm.oracleRegistry.GetAllOracles()
	
	resp := OracleListResponse{
		Oracles: oracles,
		Count:   len(oracles),
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleFeeds handles getting oracles by feed type
func (h *OracleHandler) handleFeeds(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	if h.vm.oracleRegistry == nil {
		http.Error(w, "Oracle registry not enabled", http.StatusNotImplemented)
		return
	}
	
	feedType := r.URL.Query().Get("type")
	if feedType == "" {
		http.Error(w, "Feed type parameter required", http.StatusBadRequest)
		return
	}
	
	oracles, err := h.vm.oracleRegistry.GetOraclesByFeed(feedType)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	resp := OracleListResponse{
		Oracles: oracles,
		Count:   len(oracles),
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Request/Response types

type AttestationSubmitRequest struct {
	Type       AttestationType `json:"type"`
	SourceID   string          `json:"sourceId"`
	Data       []byte          `json:"data"`
	Signatures [][]byte        `json:"signatures"`
	SignerIDs  []string        `json:"signerIds"`
	Proof      []byte          `json:"proof,omitempty"`
	Metadata   []byte          `json:"metadata,omitempty"`
}

type AttestationSubmitResponse struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type AttestationListResponse struct {
	Attestations []*Attestation `json:"attestations"`
	Count        int            `json:"count"`
}

type OracleRegisterResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	ID      string `json:"id"`
}

type OracleListResponse struct {
	Oracles []*OracleInfo `json:"oracles"`
	Count   int           `json:"count"`
}