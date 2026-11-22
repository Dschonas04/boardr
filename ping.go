package main

import (
	"context"
	"net"
	"os"
	"sync/atomic"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

var pingNummer atomic.Uint32

// ping schickt ein Echo ohne besondere Rechte (ICMP über UDP-Socket). Unter
// Linux muss net.ipv4.ping_group_range das erlauben, in Docker ist das so.
func ping(ctx context.Context, host string) error {
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	if err != nil {
		return err
	}
	if len(ips) == 0 {
		return &net.DNSError{Err: "no such host", Name: host}
	}
	c, err := icmp.ListenPacket("udp4", "0.0.0.0")
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.SetDeadline(time.Now().Add(4 * time.Second)); err != nil {
		return err
	}
	nr := int(pingNummer.Add(1) & 0xffff)
	b, err := (&icmp.Message{Type: ipv4.ICMPTypeEcho, Body: &icmp.Echo{ID: os.Getpid() & 0xffff, Seq: nr, Data: []byte("boardr")}}).Marshal(nil)
	if err != nil {
		return err
	}
	if _, err := c.WriteTo(b, &net.UDPAddr{IP: ips[0]}); err != nil {
		return err
	}
	ein := make([]byte, 1500)
	for {
		n, von, err := c.ReadFrom(ein)
		if err != nil {
			return err
		}
		u, ok := von.(*net.UDPAddr)
		if !ok || !u.IP.Equal(ips[0]) {
			continue
		}
		m, err := icmp.ParseMessage(1, ein[:n])
		if err != nil || m.Type != ipv4.ICMPTypeEchoReply {
			continue
		}
		if e, ok := m.Body.(*icmp.Echo); ok && e.Seq == nr {
			return nil
		}
	}
}
