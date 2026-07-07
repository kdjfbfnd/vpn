package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	configPath := flag.String("config", "/etc/solovpn/server.json", "path to server config")
	initConfig := flag.Bool("init-config", false, "create config if missing and exit")
	adminOnly := flag.Bool("admin-only", false, "run only the web admin panel")
	showConfig := flag.Bool("show-config", false, "print selected config values and exit")
	setPort := flag.Int("set-port", 0, "set VPN UDP port and exit")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	if *initConfig {
		log.Printf("config ready: %s", *configPath)
		log.Printf("admin user: %s", cfg.AdminUsername)
		log.Printf("admin password: %s", cfg.AdminPassword)
		return
	}
	if *showConfig {
		log.Printf("config: %s", *configPath)
		log.Printf("admin listen: %s", cfg.AdminListen)
		log.Printf("vpn listen: %s", cfg.VPNListen)
		log.Printf("vpn udp port: %d", cfg.ServerPort)
		log.Printf("public host: %s", cfg.PublicHost)
		return
	}
	if *setPort != 0 {
		if *setPort < 1 || *setPort > 65535 {
			log.Fatal("port must be 1-65535")
		}
		cfg.ServerPort = *setPort
		cfg.VPNListen = fmt.Sprintf(":%d", *setPort)
		if err := saveConfig(*configPath, cfg); err != nil {
			log.Fatal(err)
		}
		log.Printf("vpn udp port updated to %d", cfg.ServerPort)
		log.Printf("restart solovpn to apply the listening port change")
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 2)
	admin := newAdminServer(*configPath, cfg)
	go func() { errCh <- admin.Run(ctx) }()

	if !*adminOnly {
		vpn, err := newVPNServer(cfg)
		if err != nil {
			log.Fatal(err)
		}
		go func() { errCh <- vpn.Run(ctx) }()
	}

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil {
			log.Fatal(err)
		}
	}
}
