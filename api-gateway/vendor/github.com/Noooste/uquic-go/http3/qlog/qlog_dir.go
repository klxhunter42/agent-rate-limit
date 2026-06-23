package qlog

import (
	"context"

	"github.com/Noooste/uquic-go"
	"github.com/Noooste/uquic-go/qlog"
	"github.com/Noooste/uquic-go/qlogwriter"
)

const EventSchema = "urn:ietf:params:qlog:events:http3-12"

func DefaultConnectionTracer(ctx context.Context, isClient bool, connID quic.ConnectionID) qlogwriter.Trace {
	return qlog.DefaultConnectionTracerWithSchemas(ctx, isClient, connID, []string{qlog.EventSchema, EventSchema})
}
