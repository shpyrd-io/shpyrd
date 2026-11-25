package api

import (
	"context"
	"net"
	"time"
)

// proxyProtocolPreface is the PROXY protocol (version 1) line that announces
// a connection without claiming an address: the front door keeps the address
// the connection came from.
const proxyProtocolPreface = "PROXY UNKNOWN\r\n"

// announcesToFrontDoor says whether a dial to address must begin with the
// PROXY preface: the option is on and the address is not the internal front
// door, which never expects it.
func (s *Server) announcesToFrontDoor(address string) bool {
	if !s.opts.ProxyProtocol {
		return false
	}
	return address == "" || address != s.opts.IngressServiceInternal
}

// dialFrontDoor opens a connection to a controller Service, sending the
// PROXY preface first when announce is set.
func dialFrontDoor(ctx context.Context, dialer *net.Dialer, network, address string, announce bool) (net.Conn, error) {
	conn, err := dialer.DialContext(ctx, network, address)
	if err != nil || !announce {
		return conn, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetWriteDeadline(deadline)
	} else {
		_ = conn.SetWriteDeadline(time.Now().Add(dialer.Timeout + 5*time.Second))
	}
	_, err = conn.Write([]byte(proxyProtocolPreface))
	_ = conn.SetWriteDeadline(time.Time{})
	if err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}
