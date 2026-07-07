package main

import "net"

func isIPv4(packet []byte) bool {
	return len(packet) >= 20 && packet[0]>>4 == 4
}

func resolveIPv4(host string) (net.IP, error) {
	ip := net.ParseIP(host)
	if ip4 := ip.To4(); ip4 != nil {
		return ip4, nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}
	for _, candidate := range ips {
		if ip4 := candidate.To4(); ip4 != nil {
			return ip4, nil
		}
	}
	return nil, &net.DNSError{Name: host, Err: "no IPv4 address"}
}
