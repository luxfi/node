// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bft

import (
	"github.com/luxfi/bft"
	"google.golang.org/protobuf/proto"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/proto/pb/p2p"
)

func newBlockProposal(
	chainID ids.ID,
	block []byte,
	vote bft.Vote,
) *p2p.BFT {
	return &p2p.BFT{
		ChainId: chainID[:],
		Message: &p2p.BFT_BlockProposal{
			BlockProposal: &p2p.BlockProposal{
				Block: block,
				Vote: &p2p.Vote{
					BlockHeader: blockHeaderToP2P(vote.Vote.BlockHeader),
					Signature: &p2p.Signature{
						Signer: vote.Signature.Signer,
						Value:  vote.Signature.Value,
					},
				},
			},
		},
	}
}

func newVote(
	chainID ids.ID,
	vote *bft.Vote,
) *p2p.BFT {
	return &p2p.BFT{
		ChainId: chainID[:],
		Message: &p2p.BFT_Vote{
			Vote: &p2p.Vote{
				BlockHeader: blockHeaderToP2P(vote.Vote.BlockHeader),
				Signature: &p2p.Signature{
					Signer: vote.Signature.Signer,
					Value:  vote.Signature.Value,
				},
			},
		},
	}
}

func newEmptyVote(
	chainID ids.ID,
	emptyVote *bft.EmptyVote,
) *p2p.BFT {
	return &p2p.BFT{
		ChainId: chainID[:],
		Message: &p2p.BFT_EmptyVote{
			EmptyVote: &p2p.EmptyVote{
				Metadata: protocolMetadataToP2P(emptyVote.Vote.ProtocolMetadata),
				Signature: &p2p.Signature{
					Signer: emptyVote.Signature.Signer,
					Value:  emptyVote.Signature.Value,
				},
			},
		},
	}
}

func newFinalizeVote(
	chainID ids.ID,
	finalizeVote *bft.FinalizeVote,
) *p2p.BFT {
	return &p2p.BFT{
		ChainId: chainID[:],
		Message: &p2p.BFT_FinalizeVote{
			FinalizeVote: &p2p.Vote{
				BlockHeader: blockHeaderToP2P(finalizeVote.Finalization.BlockHeader),
				Signature: &p2p.Signature{
					Signer: finalizeVote.Signature.Signer,
					Value:  finalizeVote.Signature.Value,
				},
			},
		},
	}
}

func newNotarization(
	chainID ids.ID,
	notarization *bft.Notarization,
) *p2p.BFT {
	return &p2p.BFT{
		ChainId: chainID[:],
		Message: &p2p.BFT_Notarization{
			Notarization: &p2p.QuorumCertificate{
				BlockHeader:       blockHeaderToP2P(notarization.Vote.BlockHeader),
				QuorumCertificate: notarization.QC.Bytes(),
			},
		},
	}
}

func newEmptyNotarization(
	chainID ids.ID,
	emptyNotarization *bft.EmptyNotarization,
) *p2p.BFT {
	return &p2p.BFT{
		ChainId: chainID[:],
		Message: &p2p.BFT_EmptyNotarization{
			EmptyNotarization: &p2p.EmptyNotarization{
				Metadata:          protocolMetadataToP2P(emptyNotarization.Vote.ProtocolMetadata),
				QuorumCertificate: emptyNotarization.QC.Bytes(),
			},
		},
	}
}

func newFinalization(
	chainID ids.ID,
	finalization *bft.Finalization,
) *p2p.BFT {
	return &p2p.BFT{
		ChainId: chainID[:],
		Message: &p2p.BFT_Finalization{
			Finalization: &p2p.QuorumCertificate{
				BlockHeader:       blockHeaderToP2P(finalization.Finalization.BlockHeader),
				QuorumCertificate: finalization.QC.Bytes(),
			},
		},
	}
}

func newReplicationRequest(
	chainID ids.ID,
	replicationRequest *bft.ReplicationRequest,
) *p2p.BFT {
	return &p2p.BFT{
		ChainId: chainID[:],
		Message: &p2p.BFT_ReplicationRequest{
			ReplicationRequest: &p2p.ReplicationRequest{
				Seqs:        replicationRequest.Seqs,
				LatestRound: replicationRequest.LatestRound,
			},
		},
	}
}

func newReplicationResponse(
	chainID ids.ID,
	replicationResponse *bft.VerifiedReplicationResponse,
) (*p2p.BFT, error) {
	data := replicationResponse.Data
	latestRound := replicationResponse.LatestRound

	qrs := make([][]byte, 0, len(data))
	for _, qr := range data {
		p2pQR, err := quorumRoundToP2P(&qr)
		if err != nil {
			return nil, err
		}
		// Serialize the QuorumRound to bytes
		qrBytes, err := proto.Marshal(p2pQR)
		if err != nil {
			return nil, err
		}
		qrs = append(qrs, qrBytes)
	}

	latestQR, err := quorumRoundToP2P(latestRound)
	if err != nil {
		return nil, err
	}
	
	latestQRBytes, err := proto.Marshal(latestQR)
	if err != nil {
		return nil, err
	}

	return &p2p.BFT{
		ChainId: chainID[:],
		Message: &p2p.BFT_ReplicationResponse{
			ReplicationResponse: &p2p.ReplicationResponse{
				LatestQr: latestQRBytes,
				Data:        qrs,
				LatestRound: extractRoundFromVerifiedQuorumRound(latestRound),
			},
		},
	}, nil
}

func blockHeaderToP2P(bh bft.BlockHeader) *p2p.BlockHeader {
	return &p2p.BlockHeader{
		BlockId:     bh.Digest[:],
		Round:       bh.ProtocolMetadata.Round,
		ParentRound: 0, // TODO: This needs to be extracted from the BFT BlockHeader
	}
}

func protocolMetadataToP2P(md bft.ProtocolMetadata) *p2p.ProtocolMetadata {
	return &p2p.ProtocolMetadata{
		Round:      md.Round,
		ParentHash: md.Prev[:],
	}
}

func extractRoundFromVerifiedQuorumRound(qr *bft.VerifiedQuorumRound) uint64 {
	if qr.Notarization != nil {
		return qr.Notarization.Vote.BlockHeader.ProtocolMetadata.Round
	} else if qr.Finalization != nil {
		return qr.Finalization.Finalization.BlockHeader.ProtocolMetadata.Round
	} else if qr.EmptyNotarization != nil {
		return qr.EmptyNotarization.Vote.ProtocolMetadata.Round
	}
	return 0
}

func quorumRoundToP2P(qr *bft.VerifiedQuorumRound) (*p2p.QuorumRound, error) {
	// Extract the round number from the VerifiedQuorumRound
	var round uint64
	if qr.Notarization != nil {
		round = qr.Notarization.Vote.BlockHeader.ProtocolMetadata.Round
	} else if qr.Finalization != nil {
		round = qr.Finalization.Finalization.BlockHeader.ProtocolMetadata.Round
	} else if qr.EmptyNotarization != nil {
		round = qr.EmptyNotarization.Vote.ProtocolMetadata.Round
	}

	// Extract the quorum certificate bytes
	var qcBytes []byte
	if qr.Notarization != nil {
		qcBytes = qr.Notarization.QC.Bytes()
	} else if qr.Finalization != nil {
		qcBytes = qr.Finalization.QC.Bytes()
	} else if qr.EmptyNotarization != nil {
		qcBytes = qr.EmptyNotarization.QC.Bytes()
	}

	return &p2p.QuorumRound{
		QuorumCertificate: qcBytes,
		Round:             round,
	}, nil
}
