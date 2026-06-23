package ackhandler

import (
	"github.com/Noooste/uquic-go/internal/protocol"
	"github.com/Noooste/uquic-go/internal/utils"
	"github.com/Noooste/uquic-go/qlogwriter"
)

// [UQUIC]
// NewUSentPacketHandler creates a UQUIC-wrapped SentPacketHandler that supports
// custom initial packet number length for browser fingerprinting.
func NewUSentPacketHandler(
	initialPacketNumber protocol.PacketNumber,
	initialMaxDatagramSize protocol.ByteCount,
	rttStats *utils.RTTStats,
	connStats *utils.ConnectionStats,
	clientAddressValidated bool,
	enableECN bool,
	ignorePacketsBelow func(protocol.PacketNumber),
	pers protocol.Perspective,
	qlogger qlogwriter.Recorder,
	logger utils.Logger,
) SentPacketHandler {
	sph := NewSentPacketHandler(
		initialPacketNumber,
		initialMaxDatagramSize,
		rttStats,
		connStats,
		clientAddressValidated,
		enableECN,
		ignorePacketsBelow,
		pers,
		qlogger,
		logger,
	)
	return &uSentPacketHandler{
		sentPacketHandler: sph.(*sentPacketHandler),
	}
}
