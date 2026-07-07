package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"syscall"
	"time"
)

const (
	maxPacketSize   = 32767
	ringCapacity    = 0x400000
	waitObject0     = 0x00000000
	waitTimeout     = 0x00000102
	waitFailed      = 0xFFFFFFFF
	registerPeriod  = 15 * time.Second
	accountTickTime = time.Minute
)

func runTunnel(ctx context.Context, cfg *Config, apiBaseURL string, session *authSession) error {
	key, err := cfg.key()
	if err != nil {
		return err
	}
	codec, err := newPacketCodec(key)
	if err != nil {
		return err
	}
	serverIP, err := resolveIPv4(cfg.ServerHost)
	if err != nil {
		return fmt.Errorf("resolve server: %w", err)
	}
	udpAddr := &net.UDPAddr{IP: serverIP, Port: cfg.ServerPort}
	udp, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return err
	}
	defer udp.Close()

	wintun, err := loadWintun()
	if err != nil {
		return err
	}
	adapterName := cfg.ProfileName
	adapter, err := wintun.create(adapterName)
	if err != nil {
		return err
	}
	defer wintun.close(adapter)

	routes, err := configureWindowsNetwork(adapterName, cfg, serverIP)
	if err != nil {
		return err
	}
	defer routes.cleanup()

	sessionHandle, err := wintun.start(adapter, ringCapacity)
	if err != nil {
		return err
	}
	defer wintun.end(sessionHandle)

	log.Printf("connected to %s:%d as %s/%d", cfg.ServerHost, cfg.ServerPort, cfg.ClientAddress, cfg.ClientPrefixLength)
	errCh := make(chan error, 3)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		errCh <- wintunToUDP(ctx, wintun, sessionHandle, udp, codec)
	}()
	go func() {
		defer wg.Done()
		errCh <- udpToWintun(ctx, wintun, sessionHandle, udp, codec)
	}()
	go func() {
		defer wg.Done()
		errCh <- keepAlive(ctx, udp, codec, cfg.ClientAddress, apiBaseURL, session)
	}()

	select {
	case <-ctx.Done():
		_ = udp.Close()
		wg.Wait()
		return nil
	case err := <-errCh:
		_ = udp.Close()
		wg.Wait()
		return err
	}
}

func wintunToUDP(ctx context.Context, wintun *wintunDLL, session wintunSession, udp *net.UDPConn, codec *PacketCodec) error {
	event := wintun.readWaitEvent(session)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		packet, packetPtr, err := wintun.receive(session)
		if err != nil {
			if errno, ok := err.(syscall.Errno); ok && errno == 259 {
				switch result, waitErr := syscall.WaitForSingleObject(event, 1000); result {
				case waitObject0, waitTimeout:
					continue
				case waitFailed:
					return waitErr
				default:
					continue
				}
			}
			return err
		}
		if isIPv4(packet) {
			_, err = udp.Write(codec.Seal(typeData, packet))
		}
		wintun.release(session, packetPtr)
		if err != nil {
			return err
		}
	}
}

func udpToWintun(ctx context.Context, wintun *wintunDLL, session wintunSession, udp *net.UDPConn, codec *PacketCodec) error {
	buffer := make([]byte, maxPacketSize+128)
	for {
		_ = udp.SetReadDeadline(time.Now().Add(time.Second))
		n, err := udp.Read(buffer)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				select {
				case <-ctx.Done():
					return nil
				default:
					continue
				}
			}
			return err
		}
		decoded, ok := codec.Open(buffer[:n])
		if !ok || decoded.Type != typeData || !isIPv4(decoded.Payload) {
			continue
		}
		if err := wintun.send(session, decoded.Payload); err != nil {
			return err
		}
	}
}

func keepAlive(ctx context.Context, udp *net.UDPConn, codec *PacketCodec, address, apiBaseURL string, session *authSession) error {
	sendRegister := func() error {
		_, err := udp.Write(codec.Seal(typeControl, []byte("REGISTER "+address)))
		return err
	}
	if err := sendRegister(); err != nil {
		return err
	}
	registerTicker := time.NewTicker(registerPeriod)
	defer registerTicker.Stop()
	accountTicker := time.NewTicker(accountTickTime)
	defer accountTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-registerTicker.C:
			if err := sendRegister(); err != nil {
				return err
			}
		case <-accountTicker.C:
			next, err := tick(apiBaseURL, session)
			if err != nil {
				log.Printf("account tick failed: %v", err)
				continue
			}
			session = next
			if session != nil && session.RemainingMinutes <= 0 {
				return fmt.Errorf("no remaining minutes")
			}
		}
	}
}
