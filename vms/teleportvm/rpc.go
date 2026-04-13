// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package teleportvm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/luxfi/ids"
)

// =========================================================================
// JSON-RPC Handler
// =========================================================================

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      interface{}     `json:"id"`
}

type jsonRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *jsonRPCError `json:"error,omitempty"`
	ID      interface{}   `json:"id"`
}

type jsonRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type rpcHandler struct {
	vm *VM
}

func newRPCHandler(vm *VM) http.Handler {
	return &rpcHandler{vm: vm}
}

func (h *rpcHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req jsonRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, nil, -32700, "parse error", err)
		return
	}

	var result interface{}
	var err error

	switch req.Method {

	// ======== Bridge / Signer Set ========

	case "teleport.registerValidator", "teleport_registerValidator":
		var args RegisterValidatorInput
		if e := json.Unmarshal(req.Params, &args); e != nil {
			h.writeError(w, req.ID, -32602, "invalid params", e)
			return
		}
		result, err = h.vm.RegisterValidator(&args)

	case "teleport.getSignerSetInfo", "teleport_getSignerSetInfo":
		result = h.vm.GetSignerSetInfo()

	case "teleport.replaceSigner", "teleport_replaceSigner":
		var args ReplaceSignerArgs
		if e := json.Unmarshal(req.Params, &args); e != nil {
			h.writeError(w, req.ID, -32602, "invalid params", e)
			return
		}
		nodeID, e := ids.NodeIDFromString(args.NodeID)
		if e != nil {
			h.writeError(w, req.ID, -32602, "invalid nodeId", e)
			return
		}
		var replacement *ids.NodeID
		if args.ReplacementNodeID != "" {
			rid, e := ids.NodeIDFromString(args.ReplacementNodeID)
			if e != nil {
				h.writeError(w, req.ID, -32602, "invalid replacementNodeId", e)
				return
			}
			replacement = &rid
		}
		result, err = h.vm.RemoveSigner(nodeID, replacement)

	case "teleport.hasSigner", "teleport_hasSigner":
		var args HasSignerArgs
		if e := json.Unmarshal(req.Params, &args); e != nil {
			h.writeError(w, req.ID, -32602, "invalid params", e)
			return
		}
		nodeID, e := ids.NodeIDFromString(args.NodeID)
		if e != nil {
			h.writeError(w, req.ID, -32602, "invalid nodeId", e)
			return
		}
		result = map[string]bool{"isSigner": h.vm.HasSigner(nodeID)}

	case "teleport.getWaitlist", "teleport_getWaitlist":
		h.vm.mu.RLock()
		nodeIDs := make([]string, len(h.vm.signerSet.Waitlist))
		for i, nid := range h.vm.signerSet.Waitlist {
			nodeIDs[i] = nid.String()
		}
		h.vm.mu.RUnlock()
		result = map[string]interface{}{
			"waitlistSize": len(nodeIDs),
			"nodeIds":      nodeIDs,
		}

	case "teleport.getCurrentEpoch", "teleport_getCurrentEpoch":
		h.vm.mu.RLock()
		result = map[string]interface{}{
			"epoch":        h.vm.signerSet.CurrentEpoch,
			"totalSigners": len(h.vm.signerSet.Signers),
			"threshold":    h.vm.signerSet.ThresholdT,
			"setFrozen":    h.vm.signerSet.SetFrozen,
		}
		h.vm.mu.RUnlock()

	case "teleport.slashSigner", "teleport_slashSigner":
		var args SlashSignerRPCArgs
		if e := json.Unmarshal(req.Params, &args); e != nil {
			h.writeError(w, req.ID, -32602, "invalid params", e)
			return
		}
		nodeID, e := ids.NodeIDFromString(args.NodeID)
		if e != nil {
			h.writeError(w, req.ID, -32602, "invalid nodeId", e)
			return
		}
		result, err = h.vm.SlashSigner(&SlashSignerInput{
			NodeID:       nodeID,
			Reason:       args.Reason,
			SlashPercent: args.SlashPercent,
			Evidence:     []byte(args.Evidence),
		})

	case "teleport.getMPCStatus", "teleport_getMPCStatus":
		result = h.vm.GetMPCStatus()

	// ======== Relay / Channels ========

	case "teleport.openChannel", "teleport_openChannel":
		var args OpenChannelArgs
		if e := json.Unmarshal(req.Params, &args); e != nil {
			h.writeError(w, req.ID, -32602, "invalid params", e)
			return
		}
		sourceChain, e := ids.FromString(args.SourceChain)
		if e != nil {
			h.writeError(w, req.ID, -32602, "invalid sourceChain", e)
			return
		}
		destChain, e := ids.FromString(args.DestChain)
		if e != nil {
			h.writeError(w, req.ID, -32602, "invalid destChain", e)
			return
		}
		ordering := args.Ordering
		if ordering == "" {
			ordering = "unordered"
		}
		version := args.Version
		if version == "" {
			version = "1.0"
		}
		channel, e := h.vm.OpenChannel(sourceChain, destChain, ordering, version)
		if e != nil {
			err = e
		} else {
			result = map[string]string{"channelId": channel.ID.String()}
		}

	case "teleport.getChannel", "teleport_getChannel":
		var args GetChannelArgs
		if e := json.Unmarshal(req.Params, &args); e != nil {
			h.writeError(w, req.ID, -32602, "invalid params", e)
			return
		}
		channelID, e := ids.FromString(args.ChannelID)
		if e != nil {
			h.writeError(w, req.ID, -32602, "invalid channelId", e)
			return
		}
		channel, e := h.vm.GetChannel(channelID)
		if e != nil {
			err = e
		} else {
			result = channel
		}

	case "teleport.closeChannel", "teleport_closeChannel":
		var args CloseChannelArgs
		if e := json.Unmarshal(req.Params, &args); e != nil {
			h.writeError(w, req.ID, -32602, "invalid params", e)
			return
		}
		channelID, e := ids.FromString(args.ChannelID)
		if e != nil {
			h.writeError(w, req.ID, -32602, "invalid channelId", e)
			return
		}
		if e := h.vm.CloseChannel(channelID); e != nil {
			err = e
		} else {
			result = map[string]bool{"success": true}
		}

	case "teleport.listChannels", "teleport_listChannels":
		var args ListChannelsArgs
		json.Unmarshal(req.Params, &args)

		h.vm.mu.RLock()
		channels := make([]interface{}, 0, len(h.vm.channels))
		for _, ch := range h.vm.channels {
			if args.State != "" && ch.State != args.State {
				continue
			}
			channels = append(channels, ch)
		}
		h.vm.mu.RUnlock()
		result = map[string]interface{}{"channels": channels}

	case "teleport.sendMessage", "teleport_sendMessage":
		var args SendMessageArgs
		if e := json.Unmarshal(req.Params, &args); e != nil {
			h.writeError(w, req.ID, -32602, "invalid params", e)
			return
		}
		channelID, e := ids.FromString(args.ChannelID)
		if e != nil {
			h.writeError(w, req.ID, -32602, "invalid channelId", e)
			return
		}
		payload, _ := base64.StdEncoding.DecodeString(args.Payload)
		sender, _ := base64.StdEncoding.DecodeString(args.Sender)
		receiver, _ := base64.StdEncoding.DecodeString(args.Receiver)

		msg, e := h.vm.SendMessage(channelID, payload, sender, receiver, args.Timeout)
		if e != nil {
			err = e
		} else {
			result = map[string]interface{}{
				"messageId": msg.ID.String(),
				"sequence":  msg.Sequence,
			}
		}

	case "teleport.getMessage", "teleport_getMessage":
		var args GetMessageArgs
		if e := json.Unmarshal(req.Params, &args); e != nil {
			h.writeError(w, req.ID, -32602, "invalid params", e)
			return
		}
		msgID, e := ids.FromString(args.MessageID)
		if e != nil {
			h.writeError(w, req.ID, -32602, "invalid messageId", e)
			return
		}
		msg, e := h.vm.GetMessage(msgID)
		if e != nil {
			err = e
		} else {
			result = msg
		}

	case "teleport.receiveMessage", "teleport_receiveMessage":
		var args ReceiveMessageArgs
		if e := json.Unmarshal(req.Params, &args); e != nil {
			h.writeError(w, req.ID, -32602, "invalid params", e)
			return
		}
		msgID, e := ids.FromString(args.MessageID)
		if e != nil {
			h.writeError(w, req.ID, -32602, "invalid messageId", e)
			return
		}
		proof, _ := base64.StdEncoding.DecodeString(args.Proof)
		receipt, e := h.vm.ReceiveMessage(msgID, proof, args.SourceHeight)
		if e != nil {
			err = e
		} else {
			result = receipt
		}

	// ======== Oracle ========

	case "teleport.registerFeed", "teleport_registerFeed":
		var args RegisterFeedArgs
		if e := json.Unmarshal(req.Params, &args); e != nil {
			h.writeError(w, req.ID, -32602, "invalid params", e)
			return
		}
		feedBytes, _ := json.Marshal(args)
		feedID := ids.ID{}
		copy(feedID[:], feedBytes[:min(32, len(feedBytes))])

		feed := &Feed{
			ID:          feedID,
			Name:        args.Name,
			Description: args.Description,
			Sources:     args.Sources,
			Metadata:    args.Metadata,
		}
		if e := h.vm.RegisterFeed(feed); e != nil {
			err = e
		} else {
			result = map[string]string{"feedId": feedID.String()}
		}

	case "teleport.getFeed", "teleport_getFeed":
		var args GetFeedArgs
		if e := json.Unmarshal(req.Params, &args); e != nil {
			h.writeError(w, req.ID, -32602, "invalid params", e)
			return
		}
		feedID, e := ids.FromString(args.FeedID)
		if e != nil {
			h.writeError(w, req.ID, -32602, "invalid feedId", e)
			return
		}
		feed, e := h.vm.GetFeed(feedID)
		if e != nil {
			err = e
		} else {
			result = feed
		}

	case "teleport.getValue", "teleport_getValue":
		var args GetValueArgs
		if e := json.Unmarshal(req.Params, &args); e != nil {
			h.writeError(w, req.ID, -32602, "invalid params", e)
			return
		}
		feedID, e := ids.FromString(args.FeedID)
		if e != nil {
			h.writeError(w, req.ID, -32602, "invalid feedId", e)
			return
		}
		value, e := h.vm.GetLatestValue(feedID)
		if e != nil {
			err = e
		} else {
			result = value
		}

	case "teleport.submitObservation", "teleport_submitObservation":
		var args SubmitObservationArgs
		if e := json.Unmarshal(req.Params, &args); e != nil {
			h.writeError(w, req.ID, -32602, "invalid params", e)
			return
		}
		feedID, e := ids.FromString(args.FeedID)
		if e != nil {
			h.writeError(w, req.ID, -32602, "invalid feedId", e)
			return
		}
		obs := &Observation{
			FeedID:    feedID,
			Value:     args.Value,
			Signature: args.Signature,
		}
		if e := h.vm.SubmitObservation(obs); e != nil {
			err = e
		} else {
			result = map[string]bool{"success": true}
		}

	case "teleport.getAttestation", "teleport_getAttestation":
		var args GetAttestationArgs
		if e := json.Unmarshal(req.Params, &args); e != nil {
			h.writeError(w, req.ID, -32602, "invalid params", e)
			return
		}
		feedID, e := ids.FromString(args.FeedID)
		if e != nil {
			h.writeError(w, req.ID, -32602, "invalid feedId", e)
			return
		}
		att, e := h.vm.CreateAttestation(feedID, args.Epoch)
		if e != nil {
			err = e
		} else {
			result = map[string]interface{}{
				"attestation": att.Bytes(),
			}
		}

	// ======== Health ========

	case "teleport.health", "teleport_health":
		health, e := h.vm.HealthCheck(context.Background())
		if e != nil {
			err = e
		} else {
			result = health
		}

	default:
		h.writeError(w, req.ID, -32601, "method not found", nil)
		return
	}

	if err != nil {
		h.writeError(w, req.ID, -32000, "server error", err)
		return
	}

	h.writeResult(w, req.ID, result)
}

func (h *rpcHandler) writeResult(w http.ResponseWriter, id interface{}, result interface{}) {
	resp := jsonRPCResponse{
		JSONRPC: "2.0",
		Result:  result,
		ID:      id,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *rpcHandler) writeError(w http.ResponseWriter, id interface{}, code int, message string, data interface{}) {
	resp := jsonRPCResponse{
		JSONRPC: "2.0",
		Error: &jsonRPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
		ID: id,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// =========================================================================
// RPC Argument Types
// =========================================================================

// Bridge / Signer
type ReplaceSignerArgs struct {
	NodeID            string `json:"nodeId"`
	ReplacementNodeID string `json:"replacementNodeId"`
}

type HasSignerArgs struct {
	NodeID string `json:"nodeId"`
}

type SlashSignerRPCArgs struct {
	NodeID       string `json:"nodeId"`
	Reason       string `json:"reason"`
	SlashPercent int    `json:"slashPercent"`
	Evidence     string `json:"evidence"`
}

// Relay
type OpenChannelArgs struct {
	SourceChain string `json:"sourceChain"`
	DestChain   string `json:"destChain"`
	Ordering    string `json:"ordering"`
	Version     string `json:"version"`
}

type GetChannelArgs struct {
	ChannelID string `json:"channelId"`
}

type CloseChannelArgs struct {
	ChannelID string `json:"channelId"`
}

type ListChannelsArgs struct {
	State string `json:"state"`
}

type SendMessageArgs struct {
	ChannelID string `json:"channelId"`
	Payload   string `json:"payload"`
	Sender    string `json:"sender"`
	Receiver  string `json:"receiver"`
	Timeout   int64  `json:"timeout"`
}

type GetMessageArgs struct {
	MessageID string `json:"messageId"`
}

type ReceiveMessageArgs struct {
	MessageID    string `json:"messageId"`
	Proof        string `json:"proof"`
	SourceHeight uint64 `json:"sourceHeight"`
}

// Oracle
type RegisterFeedArgs struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Sources     []string          `json:"sources"`
	UpdateFreq  string            `json:"updateFreq"`
	Operators   []string          `json:"operators"`
	Metadata    map[string]string `json:"metadata"`
}

type GetFeedArgs struct {
	FeedID string `json:"feedId"`
}

type GetValueArgs struct {
	FeedID string `json:"feedId"`
}

type SubmitObservationArgs struct {
	FeedID    string `json:"feedId"`
	Value     []byte `json:"value"`
	Signature []byte `json:"signature"`
}

type GetAttestationArgs struct {
	FeedID string `json:"feedId"`
	Epoch  uint64 `json:"epoch"`
}
