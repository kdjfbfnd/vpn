package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
)

type Config struct {
	ProfileName        string `json:"profileName"`
	ServerHost         string `json:"serverHost"`
	ServerPort         int    `json:"serverPort"`
	ClientAddress      string `json:"clientAddress"`
	ClientPrefixLength int    `json:"clientPrefixLength"`
	DNS                string `json:"dns"`
	MTU                int    `json:"mtu"`
	SharedKey          string `json:"sharedKey"`
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := new(Config)
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, cfg.validate()
}

func (c *Config) validate() error {
	c.ProfileName = strings.TrimSpace(c.ProfileName)
	c.ServerHost = strings.TrimSpace(c.ServerHost)
	c.ClientAddress = strings.TrimSpace(c.ClientAddress)
	c.DNS = strings.TrimSpace(c.DNS)
	c.SharedKey = strings.TrimSpace(c.SharedKey)
	if c.ProfileName == "" {
		c.ProfileName = "Solo VPN"
	}
	if c.ServerHost == "" {
		return fmt.Errorf("serverHost is required")
	}
	if c.ServerPort < 1 || c.ServerPort > 65535 {
		return fmt.Errorf("serverPort must be 1-65535")
	}
	if net.ParseIP(c.ClientAddress).To4() == nil {
		return fmt.Errorf("clientAddress must be IPv4")
	}
	if c.ClientPrefixLength < 1 || c.ClientPrefixLength > 32 {
		return fmt.Errorf("clientPrefixLength must be 1-32")
	}
	if net.ParseIP(c.DNS).To4() == nil {
		return fmt.Errorf("dns must be IPv4")
	}
	if c.MTU < 576 || c.MTU > 1500 {
		return fmt.Errorf("mtu must be 576-1500")
	}
	key, err := base64.StdEncoding.DecodeString(c.SharedKey)
	if err != nil || len(key) != 32 {
		return fmt.Errorf("sharedKey must be 32 bytes Base64")
	}
	return nil
}

func (c *Config) key() ([]byte, error) {
	return base64.StdEncoding.DecodeString(c.SharedKey)
}

func subnetMask(prefix int) (string, error) {
	mask := net.CIDRMask(prefix, 32)
	if mask == nil {
		return "", fmt.Errorf("invalid IPv4 prefix %d", prefix)
	}
	return net.IP(mask).String(), nil
}
