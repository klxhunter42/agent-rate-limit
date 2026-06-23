//go:build !go1.17
// +build !go1.17

package websocket

import (
	"context"
	"github.com/Noooste/utls"
)

func doHandshake(ctx context.Context, tlsConn *tls.Conn, cfg *tls.Config) error {
	if err := tlsConn.Handshake(); err != nil {
		return err
	}
	if !cfg.InsecureSkipVerify {
		if err := tlsConn.VerifyHostname(cfg.ServerName); err != nil {
			return err
		}
	}
	return nil
}
