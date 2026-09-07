// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"errors"
	"strconv"

	"github.com/luxfi/metric"

	"github.com/luxfi/node/message"
)

const (
	ioLabel         = "io"
	opLabel         = "op"
	compressedLabel = "compressed"

	sentLabel     = "sent"
	receivedLabel = "received"
)

var (
	opLabels             = []string{opLabel}
	ioOpLabels           = []string{ioLabel, opLabel}
	ioOpCompressedLabels = []string{ioLabel, opLabel, compressedLabel}
	chainMismatchLabels  = []string{"peer", "chain", "local_genesis", "peer_genesis"}
)

type Metrics struct {
	ClockSkewCount metric.Counter
	ClockSkewSum   metric.Gauge

	NumFailedToParse metric.Counter
	NumSendFailed    metric.CounterVec // op

	Messages   metric.CounterVec // io + op + compressed
	Bytes      metric.CounterVec // io + op
	BytesSaved metric.GaugeVec   // io + op

	// ChainIdentityMismatch names both sides of a disagreement, because the
	// question an operator has is never "did one happen" but "which of these two
	// nodes is on the wrong chain", and that is unanswerable without the digests.
	// The peer and digest labels are unbounded in principle; in practice this
	// series only ever appears when something is misconfigured, and a fleet where
	// it is high-cardinality has a much larger problem than its metrics.
	ChainIdentityMismatch metric.GaugeVec // peer + chain + local_genesis + peer_genesis
	// ChainDivergentMsgs counts messages dropped in either direction because the
	// peer is on a different chain. A number that climbs is a peer still trying.
	ChainDivergentMsgs metric.Counter
	// ChainRulesDiffer counts peers on the same chain running a different rule
	// generation: compatible now, scheduled to diverge later.
	ChainRulesDiffer metric.Counter
}

func NewMetrics(registerer metric.Registerer) (*Metrics, error) {
	if registerer == nil {
		registerer = metric.NewNoOpRegistry()
	}
	m := &Metrics{
		ClockSkewCount: registerer.NewCounter("clock_skew_count", "number of handshake timestamps inspected (n)"),
		ClockSkewSum:   registerer.NewGauge("clock_skew_sum", "sum of (peer timestamp - local timestamp) from handshake messages (s)"),
		NumFailedToParse: registerer.NewCounter("msgs_failed_to_parse",
			"number of received messages that could not be parsed"),
		NumSendFailed: registerer.NewCounterVec("msgs_failed_to_send",
			"number of messages that failed to be sent", opLabels),
		Messages: registerer.NewCounterVec("msgs",
			"number of handled messages", ioOpCompressedLabels),
		Bytes: registerer.NewCounterVec("msgs_bytes",
			"number of message bytes", ioOpLabels),
		BytesSaved: registerer.NewGaugeVec("msgs_bytes_saved",
			"number of message bytes saved", ioOpLabels),
		ChainIdentityMismatch: registerer.NewGaugeVec("chain_identity_mismatch",
			"1 while a peer states a different chain for a blockchain this node runs",
			chainMismatchLabels),
		ChainDivergentMsgs: registerer.NewCounter("chain_divergent_msgs",
			"number of messages dropped because the peer is on a different chain"),
		ChainRulesDiffer: registerer.NewCounter("chain_rules_differ",
			"number of peers on the same chain under a different rule generation"),
	}
	return m, errors.Join()
}

// Sent updates the metrics for having sent [msg].
func (m *Metrics) Sent(msg message.OutboundMessage) {
	op := msg.Op().String()
	saved := msg.BytesSavedCompression()
	compressed := saved != 0 // assume that if [saved] == 0, [msg] wasn't compressed
	compressedStr := strconv.FormatBool(compressed)

	m.Messages.With(metric.Labels{
		ioLabel:         sentLabel,
		opLabel:         op,
		compressedLabel: compressedStr,
	}).Inc()

	bytesLabel := metric.Labels{
		ioLabel: sentLabel,
		opLabel: op,
	}
	m.Bytes.With(bytesLabel).Add(float64(len(msg.Bytes())))
	m.BytesSaved.With(bytesLabel).Add(float64(saved))
}

func (m *Metrics) MultipleSendsFailed(op message.Op, count int) {
	m.NumSendFailed.With(metric.Labels{
		opLabel: op.String(),
	}).Add(float64(count))
}

// SendFailed updates the metrics for having failed to send [msg].
func (m *Metrics) SendFailed(msg message.OutboundMessage) {
	op := msg.Op().String()
	m.NumSendFailed.With(metric.Labels{
		opLabel: op,
	}).Inc()
}

func (m *Metrics) Received(msg message.InboundMessage, msgLen uint32) {
	op := msg.Op().String()
	saved := msg.BytesSavedCompression()
	compressed := saved != 0 // assume that if [saved] == 0, [msg] wasn't compressed
	compressedStr := strconv.FormatBool(compressed)

	m.Messages.With(metric.Labels{
		ioLabel:         receivedLabel,
		opLabel:         op,
		compressedLabel: compressedStr,
	}).Inc()

	bytesLabel := metric.Labels{
		ioLabel: receivedLabel,
		opLabel: op,
	}
	m.Bytes.With(bytesLabel).Add(float64(msgLen))
	m.BytesSaved.With(bytesLabel).Add(float64(saved))
}
