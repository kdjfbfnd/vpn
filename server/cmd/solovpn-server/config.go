package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	VPNListen          string `json:"vpnListen"`
	AdminListen        string `json:"adminListen"`
	AdminUsername      string `json:"adminUsername"`
	AdminPassword      string `json:"adminPassword"`
	PublicHost         string `json:"publicHost"`
	ServerPort         int    `json:"serverPort"`
	TunName            string `json:"tunName"`
	TunCIDR            string `json:"tunCidr"`
	ClientAddress      string `json:"clientAddress"`
	DNS                string `json:"dns"`
	MTU                int    `json:"mtu"`
	SharedKey          string `json:"sharedKey"`
	AndroidProjectPath string `json:"androidProjectPath"`
	APKOutputDir       string `json:"apkOutputDir"`
	Users              map[string]UserAccount `json:"users,omitempty"`
	AppName            string `json:"appName"`
	AppPackageName     string `json:"appPackageName"`
	AppVersionCode     int    `json:"appVersionCode"`
	AppVersionName     string `json:"appVersionName"`
	AppThemePrimary    string `json:"appThemePrimary"`
	AppThemeAccent     string `json:"appThemeAccent"`
	AppIconStartColor  string `json:"appIconStartColor"`
	AppIconEndColor    string `json:"appIconEndColor"`
	AppIconAccentColor string `json:"appIconAccentColor"`
	AppIconUploadPath  string `json:"appIconUploadPath"`
	APKFilePrefix      string `json:"apkFilePrefix"`
}

type UserAccount struct {
	Username         string `json:"username"`
	PasswordSalt     string `json:"passwordSalt"`
	PasswordHash     string `json:"passwordHash"`
	Token            string `json:"token,omitempty"`
	RemainingMinutes int    `json:"remainingMinutes"`
}

func defaultConfig() (*Config, error) {
	key, err := randomBase64(32)
	if err != nil {
		return nil, err
	}
	password, err := randomBase64(18)
	if err != nil {
		return nil, err
	}

	return &Config{
		VPNListen:          ":51820",
		AdminListen:        ":8080",
		AdminUsername:      "admin",
		AdminPassword:      password,
		PublicHost:         "",
		ServerPort:         51820,
		TunName:            "solo0",
		TunCIDR:            "10.66.0.1/24",
		ClientAddress:      "10.66.0.2",
		DNS:                "1.1.1.1",
		MTU:                1280,
		SharedKey:          key,
		AndroidProjectPath: "/opt/solovpn/project",
		APKOutputDir:       "/opt/solovpn/builds",
		Users:              map[string]UserAccount{},
		AppName:            "极速传 VPN",
		AppPackageName:     "com.example.myapplica",
		AppVersionCode:     1,
		AppVersionName:     "1.0",
		AppThemePrimary:    "#126CFF",
		AppThemeAccent:     "#16A085",
		AppIconStartColor:  "#7DD3FC",
		AppIconEndColor:    "#B7F3D0",
		AppIconAccentColor: "#35CBA5",
		APKFilePrefix:      "solovpn",
	}, nil
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		cfg, err := defaultConfig()
		if err != nil {
			return nil, err
		}
		return cfg, saveConfig(path, cfg)
	}
	if err != nil {
		return nil, err
	}

	cfg := new(Config)
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	if cfg.ServerPort == 0 {
		cfg.ServerPort = portFromListen(cfg.VPNListen, 51820)
	}
	if cfg.Users == nil {
		cfg.Users = map[string]UserAccount{}
	}
	cfg.applyBuildDefaults()
	return cfg, cfg.validate()
}

func saveConfig(path string, cfg *Config) error {
	cfg.applyBuildDefaults()
	if err := cfg.validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func (c *Config) validate() error {
	if strings.TrimSpace(c.VPNListen) == "" {
		return fmt.Errorf("vpnListen is required")
	}
	if strings.TrimSpace(c.AdminListen) == "" {
		return fmt.Errorf("adminListen is required")
	}
	if c.ServerPort <= 0 || c.ServerPort > 65535 {
		return fmt.Errorf("serverPort must be 1-65535")
	}
	if strings.TrimSpace(c.TunName) == "" {
		return fmt.Errorf("tunName is required")
	}
	if _, _, err := net.ParseCIDR(c.TunCIDR); err != nil {
		return fmt.Errorf("tunCidr is invalid: %w", err)
	}
	if net.ParseIP(c.ClientAddress).To4() == nil {
		return fmt.Errorf("clientAddress must be IPv4")
	}
	if net.ParseIP(c.DNS).To4() == nil {
		return fmt.Errorf("dns must be IPv4")
	}
	if c.MTU < 576 || c.MTU > 1500 {
		return fmt.Errorf("mtu must be 576-1500")
	}
	key, err := base64.StdEncoding.DecodeString(c.SharedKey)
	if err != nil || len(key) != 32 {
		return fmt.Errorf("sharedKey must be 32 bytes base64")
	}
	if strings.TrimSpace(c.AndroidProjectPath) == "" {
		return fmt.Errorf("androidProjectPath is required")
	}
	if strings.TrimSpace(c.APKOutputDir) == "" {
		return fmt.Errorf("apkOutputDir is required")
	}
	if strings.TrimSpace(c.AppName) == "" {
		return fmt.Errorf("appName is required")
	}
	if !validPackageName(c.AppPackageName) {
		return fmt.Errorf("appPackageName is invalid")
	}
	if c.AppVersionCode <= 0 {
		return fmt.Errorf("appVersionCode must be positive")
	}
	if strings.TrimSpace(c.AppVersionName) == "" {
		return fmt.Errorf("appVersionName is required")
	}
	for name, color := range map[string]string{
		"appThemePrimary":    c.AppThemePrimary,
		"appThemeAccent":     c.AppThemeAccent,
		"appIconStartColor":  c.AppIconStartColor,
		"appIconEndColor":    c.AppIconEndColor,
		"appIconAccentColor": c.AppIconAccentColor,
	} {
		if !validHexColor(color) {
			return fmt.Errorf("%s must be #RRGGBB", name)
		}
	}
	return nil
}

func (c *Config) applyBuildDefaults() {
	if strings.TrimSpace(c.AppName) == "" {
		c.AppName = "极速传 VPN"
	}
	if strings.TrimSpace(c.AppPackageName) == "" {
		c.AppPackageName = "com.example.myapplica"
	}
	if c.AppVersionCode <= 0 {
		c.AppVersionCode = 1
	}
	if strings.TrimSpace(c.AppVersionName) == "" {
		c.AppVersionName = "1.0"
	}
	if strings.TrimSpace(c.AppThemePrimary) == "" {
		c.AppThemePrimary = "#126CFF"
	}
	if strings.TrimSpace(c.AppThemeAccent) == "" {
		c.AppThemeAccent = "#16A085"
	}
	if strings.TrimSpace(c.AppIconStartColor) == "" {
		c.AppIconStartColor = "#7DD3FC"
	}
	if strings.TrimSpace(c.AppIconEndColor) == "" {
		c.AppIconEndColor = "#B7F3D0"
	}
	if strings.TrimSpace(c.AppIconAccentColor) == "" {
		c.AppIconAccentColor = "#35CBA5"
	}
	if strings.TrimSpace(c.APKFilePrefix) == "" {
		c.APKFilePrefix = "solovpn"
	}
}

func validPackageName(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for i, r := range part {
			if i == 0 {
				if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && r != '_' {
					return false
				}
				continue
			}
			if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '_' {
				return false
			}
		}
	}
	return true
}

func validHexColor(value string) bool {
	if len(value) != 7 || value[0] != '#' {
		return false
	}
	for _, r := range value[1:] {
		if !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'f') && !(r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

func randomBase64(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

func portFromListen(listen string, fallback int) int {
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(port, "%d", &parsed); err != nil || parsed == 0 {
		return fallback
	}
	return parsed
}
