package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	configPath := flag.String("config", "/etc/solovpn/server.json", "path to server config")
	initConfig := flag.Bool("init-config", false, "create config if missing and exit")
	adminOnly := flag.Bool("admin-only", false, "run only the web admin panel")
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
