package main

import "net"

func isIPv4(packet []byte) bool {
	return len(packet) >= 20 && packet[0]>>4 == 4
}

func ipv4Source(packet []byte) net.IP {
	if !isIPv4(packet) {
		return nil
	}
	return net.IPv4(packet[12], packet[13], packet[14], packet[15])
}

func ipv4Destination(packet []byte) net.IP {
	if !isIPv4(packet) {
		return nil
	}
	return net.IPv4(packet[16], packet[17], packet[18], packet[19])
}
