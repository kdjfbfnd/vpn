//go:build linux

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"
)

const (
	iffTun    = 0x0001
	iffNoPI   = 0x1000
	tunSetIFF = 0x400454ca
)

func openTun(name string) (*os.File, error) {
	tun, err := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}

	var ifr [40]byte
	copy(ifr[:16], []byte(name))
	binary.LittleEndian.PutUint16(ifr[16:18], iffTun|iffNoPI)

	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		tun.Fd(),
		uintptr(tunSetIFF),
		uintptr(unsafe.Pointer(&ifr[0])),
	)
	if errno != 0 {
		_ = tun.Close()
		return nil, errno
	}
	return tun, nil
}

func configureTun(cfg *Config) error {
	commands := [][]string{
		{"ip", "addr", "add", cfg.TunCIDR, "dev", cfg.TunName},
		{"ip", "link", "set", "dev", cfg.TunName, "mtu", fmt.Sprint(cfg.MTU), "up"},
	}

	for _, args := range commands {
		cmd := exec.Command(args[0], args[1:]...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			text := string(output)
			if bytes.Contains(output, []byte("File exists")) || strings.Contains(text, "RTNETLINK answers: File exists") {
				continue
			}
			return fmt.Errorf("%s failed: %w: %s", strings.Join(args, " "), err, text)
		}
		if len(output) > 0 {
			log.Printf("%s", strings.TrimSpace(string(output)))
		}
	}
	return nil
}
