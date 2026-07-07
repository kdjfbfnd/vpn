package main

import (
	"bytes"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type routeState struct {
	adapterName string
	ifIndex     int
	serverIP    net.IP
	gateway     string
	addedHost   bool
	addedAll    bool
}

func configureWindowsNetwork(adapterName string, cfg *Config, serverIP net.IP) (*routeState, error) {
	mask, err := subnetMask(cfg.ClientPrefixLength)
	if err != nil {
		return nil, err
	}
	if err := run("netsh", "interface", "ip", "set", "address", "name="+adapterName, "static", cfg.ClientAddress, mask); err != nil {
		return nil, err
	}
	_ = run("netsh", "interface", "ip", "set", "dns", "name="+adapterName, "static", cfg.DNS)
	_ = run("netsh", "interface", "ipv4", "set", "subinterface", adapterName, "mtu="+strconv.Itoa(cfg.MTU), "store=active")

	iface, err := waitInterface(adapterName, 10*time.Second)
	if err != nil {
		return nil, err
	}
	state := &routeState{adapterName: adapterName, ifIndex: iface.Index, serverIP: serverIP}
	if gateway := defaultGateway(); gateway != "" {
		if err := run("route", "add", serverIP.String(), "mask", "255.255.255.255", gateway, "metric", "1"); err == nil {
			state.gateway = gateway
			state.addedHost = true
		}
	}
	if err := run("route", "add", "0.0.0.0", "mask", "0.0.0.0", "0.0.0.0", "if", strconv.Itoa(iface.Index), "metric", "1"); err != nil {
		state.cleanup()
		return nil, err
	}
	state.addedAll = true
	return state, nil
}

func (s *routeState) cleanup() {
	if s == nil {
		return
	}
	if s.addedAll {
		_ = run("route", "delete", "0.0.0.0", "mask", "0.0.0.0", "0.0.0.0")
	}
	if s.addedHost && s.serverIP != nil {
		_ = run("route", "delete", s.serverIP.String(), "mask", "255.255.255.255")
	}
}

func waitInterface(name string, timeout time.Duration) (*net.Interface, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		iface, err := net.InterfaceByName(name)
		if err == nil {
			return iface, nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return nil, fmt.Errorf("network interface %q was not found", name)
}

func defaultGateway() string {
	cmd := exec.Command("route", "print", "-4", "0.0.0.0")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`(?m)^\s*0\.0\.0\.0\s+0\.0\.0\.0\s+([0-9.]+)\s+([0-9.]+)\s+\d+`)
	matches := re.FindSubmatch(output)
	if len(matches) >= 2 {
		gateway := string(matches[1])
		if gateway != "0.0.0.0" {
			return gateway
		}
	}
	return ""
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		text := strings.TrimSpace(stderr.String())
		if text == "" {
			text = err.Error()
		}
		return fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), text)
	}
	return nil
}
