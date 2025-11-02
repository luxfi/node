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
)

type Metrics struct {
	ClockSkewCount metric.Counter
	ClockSkewSum   metric.Gauge

	NumFailedToParse metric.Counter
	NumSendFailed    metric.CounterVec // op

	Messages   metric.CounterVec // io + op + compressed
	Bytes      metric.CounterVec // io + op
	BytesSaved metric.GaugeVec   // io + op
}

func NewMetrics(registerer metric.Registerer) (*Metrics, error) {
	m := &Metrics{
		ClockSkewCount: metric.NewCounter(metric.CounterOpts{
			Name: "clock_skew_count",
			Help: "number of handshake timestamps inspected (n)",
		}),
		ClockSkewSum: metric.NewGauge(metric.GaugeOpts{
			Name: "clock_skew_sum",
			Help: "sum of (peer timestamp - local timestamp) from handshake messages (s)",
		}),
		NumFailedToParse: metric.NewCounter(metric.CounterOpts{
			Name: "msgs_failed_to_parse",
			Help: "number of received messages that could not be parsed",
		}),
		NumSendFailed: metric.NewCounterVec(
			metric.CounterOpts{
				Name: "msgs_failed_to_send",
				Help: "number of messages that failed to be sent",
			},
			opLabels,
		),
		Messages: metric.NewCounterVec(
			metric.CounterOpts{
				Name: "msgs",
				Help: "number of handled messages",
			},
			ioOpCompressedLabels,
		),
		Bytes: metric.NewCounterVec(
			metric.CounterOpts{
				Name: "msgs_bytes",
				Help: "number of message bytes",
			},
			ioOpLabels,
		),
		BytesSaved: metric.NewGaugeVec(
			metric.GaugeOpts{
				Name: "msgs_bytes_saved",
				Help: "number of message bytes saved",
			},
			ioOpLabels,
		),
	}
	return m, errors.Join(
	)
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
