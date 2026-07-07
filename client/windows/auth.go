package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type authSession struct {
	Token            string `json:"token"`
	Username         string `json:"username"`
	RemainingMinutes int    `json:"remainingMinutes"`
}

type apiConfigResponse struct {
	Config
	RemainingMinutes int `json:"remainingMinutes"`
}

func loginAndFetchConfig(apiBaseURL, username, password string) (*Config, *authSession, error) {
	apiBaseURL = strings.TrimRight(strings.TrimSpace(apiBaseURL), "/")
	if apiBaseURL == "" {
		return nil, nil, fmt.Errorf("api base URL is required")
	}
	session, err := login(apiBaseURL, username, password)
	if err != nil {
		return nil, nil, err
	}
	cfg, remaining, err := fetchConfig(apiBaseURL, session)
	if err != nil {
		return nil, nil, err
	}
	session.RemainingMinutes = remaining
	return cfg, session, nil
}

func login(apiBaseURL, username, password string) (*authSession, error) {
	return auth(apiBaseURL, "/api/login", username, password)
}

func register(apiBaseURL, username, password string) (*authSession, error) {
	return auth(apiBaseURL, "/api/register", username, password)
}

func auth(apiBaseURL, path, username, password string) (*authSession, error) {
	body, _ := json.Marshal(map[string]string{
		"username": strings.TrimSpace(username),
		"password": password,
	})
	req, err := http.NewRequest(http.MethodPost, apiBaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	var session authSession
	if err := doJSON(req, &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func fetchConfig(apiBaseURL string, session *authSession) (*Config, int, error) {
	req, err := http.NewRequest(http.MethodGet, apiBaseURL+"/api/config", nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+session.Username+":"+session.Token)

	var res apiConfigResponse
	if err := doJSON(req, &res); err != nil {
		return nil, 0, err
	}
	cfg := res.Config
	return &cfg, res.RemainingMinutes, cfg.validate()
}

func me(apiBaseURL string, session *authSession) (*authSession, error) {
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(apiBaseURL, "/")+"/api/me", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+session.Username+":"+session.Token)

	var next authSession
	if err := doJSON(req, &next); err != nil {
		return nil, err
	}
	return &next, nil
}

func tick(apiBaseURL string, session *authSession) (*authSession, error) {
	if session == nil || strings.TrimSpace(apiBaseURL) == "" {
		return session, nil
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(apiBaseURL, "/")+"/api/tick", bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+session.Username+":"+session.Token)

	var next authSession
	if err := doJSON(req, &next); err != nil {
		return nil, err
	}
	return &next, nil
}

func doJSON(req *http.Request, target any) error {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var body struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &body)
		if body.Error != "" {
			return fmt.Errorf("%s", body.Error)
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	return json.Unmarshal(data, target)
}
