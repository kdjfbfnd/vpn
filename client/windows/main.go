package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
)

var defaultAPIBaseURL string

func main() {
	if len(os.Args) == 1 {
		runGUI(defaultAPIBaseURL)
		return
	}

	configPath := flag.String("config", "solovpn-client.json", "path to client config JSON")
	apiBaseURL := flag.String("api", defaultAPIBaseURL, "admin API base URL, for example http://1.2.3.4:8080")
	username := flag.String("username", "", "account username when using -api")
	password := flag.String("password", "", "account password when using -api")
	flag.Parse()

	cfg, session, err := selectConfig(*configPath, *apiBaseURL, *username, *password)
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := runTunnel(ctx, cfg, strings.TrimRight(*apiBaseURL, "/"), session); err != nil {
		log.Fatal(err)
	}
}

func selectConfig(path, apiBaseURL, username, password string) (*Config, *authSession, error) {
	if strings.TrimSpace(apiBaseURL) != "" {
		if strings.TrimSpace(username) == "" || password == "" {
			return nil, nil, fmt.Errorf("-username and -password are required with -api")
		}
		return loginAndFetchConfig(apiBaseURL, username, password)
	}
	cfg, err := loadConfig(path)
	return cfg, nil, err
}
