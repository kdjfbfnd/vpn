package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
)

type VPNServer struct {
	cfg     *Config
	codec   *PacketCodec
	tun     *os.File
	udp     *net.UDPConn
	clients map[string]*net.UDPAddr
	mu      sync.RWMutex
}

func newVPNServer(cfg *Config) (*VPNServer, error) {
	codec, err := newPacketCodec(cfg.SharedKey)
	if err != nil {
		return nil, err
	}
	return &VPNServer{
		cfg:     cfg,
		codec:   codec,
		clients: map[string]*net.UDPAddr{},
	}, nil
}

func (s *VPNServer) Run(ctx context.Context) error {
	tun, err := openTun(s.cfg.TunName)
	if err != nil {
		return fmt.Errorf("open tun: %w", err)
	}
	s.tun = tun
	if err := configureTun(s.cfg); err != nil {
		return err
	}

	addr, err := net.ResolveUDPAddr("udp", s.cfg.VPNListen)
	if err != nil {
		return err
	}
	udp, err := net.ListenUDP("udp", addr)
	if err != nil {
		return err
	}
	s.udp = udp

	errCh := make(chan error, 2)
	go func() { errCh <- s.udpLoop(ctx) }()
	go func() { errCh <- s.tunLoop(ctx) }()

	log.Printf("vpn listening on udp %s, tun %s %s", s.cfg.VPNListen, s.cfg.TunName, s.cfg.TunCIDR)

	select {
	case <-ctx.Done():
		_ = udp.Close()
		_ = tun.Close()
		return nil
	case err := <-errCh:
		_ = udp.Close()
		_ = tun.Close()
		return err
	}
}

func (s *VPNServer) udpLoop(ctx context.Context) error {
	buffer := make([]byte, 65535)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		n, addr, err := s.udp.ReadFromUDP(buffer)
		if err != nil {
			if ctx.Err() != nil || isClosedNetErr(err) {
				return nil
			}
			return err
		}

		decoded, ok := s.codec.Open(buffer[:n])
		if !ok {
			continue
		}

		switch decoded.Type {
		case typeControl:
			s.handleControl(addr, decoded.Payload)
		case typeData:
			if !isIPv4(decoded.Payload) {
				continue
			}
			src := ipv4Source(decoded.Payload)
			if src != nil {
				s.registerClient(src.String(), addr)
			}
			if _, err := s.tun.Write(decoded.Payload); err != nil {
				return err
			}
		}
	}
}

func (s *VPNServer) tunLoop(ctx context.Context) error {
	buffer := make([]byte, 65535)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		n, err := s.tun.Read(buffer)
		if err != nil {
			if err == io.EOF || ctx.Err() != nil {
				return nil
			}
			return err
		}
		packet := buffer[:n]
		if !isIPv4(packet) {
			continue
		}
		dst := ipv4Destination(packet)
		if dst == nil {
			continue
		}

		addr := s.clientAddr(dst.String())
		if addr == nil {
			continue
		}
		sealed := s.codec.Seal(typeData, packet)
		if _, err := s.udp.WriteToUDP(sealed, addr); err != nil {
			return err
		}
	}
}

func (s *VPNServer) handleControl(addr *net.UDPAddr, payload []byte) {
	text := strings.TrimSpace(string(payload))
	if !strings.HasPrefix(text, "REGISTER ") {
		return
	}
	ip := strings.TrimSpace(strings.TrimPrefix(text, "REGISTER "))
	if net.ParseIP(ip).To4() == nil {
		return
	}
	s.registerClient(ip, addr)
}

func (s *VPNServer) registerClient(ip string, addr *net.UDPAddr) {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.clients[ip]
	s.clients[ip] = addr
	if previous == nil || previous.String() != addr.String() {
		log.Printf("client %s registered from %s", ip, addr)
	}
}

func (s *VPNServer) clientAddr(ip string) *net.UDPAddr {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.clients[ip]
}

func isClosedNetErr(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	return strings.Contains(text, "use of closed network connection") || strings.Contains(text, "file already closed")
}
