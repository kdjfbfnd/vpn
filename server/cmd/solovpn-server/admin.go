package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type AdminServer struct {
	cfgPath string
	cfg     *Config
	mu      sync.Mutex

	buildMu      sync.Mutex
	buildRunning bool
	lastLog      string
	lastAPK      string
	lastWindows  string
}

type androidAssetConfig struct {
	APIBaseURL string `json:"apiBaseUrl"`
}

func newAdminServer(cfgPath string, cfg *Config) *AdminServer {
	return &AdminServer{cfgPath: cfgPath, cfg: cfg}
}

func (a *AdminServer) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/register", a.apiRegister)
	mux.HandleFunc("/api/login", a.apiLogin)
	mux.HandleFunc("/api/me", a.apiMe)
	mux.HandleFunc("/api/config", a.apiConfig)
	mux.HandleFunc("/api/tick", a.apiTick)
	mux.HandleFunc("/", a.withAuth(a.index))
	mux.HandleFunc("/settings", a.withAuth(a.updateSettings))
	mux.HandleFunc("/icon", a.withAuth(a.uploadIcon))
	mux.HandleFunc("/users", a.withAuth(a.updateUserMinutes))
	mux.HandleFunc("/build", a.withAuth(a.startBuild))
	mux.HandleFunc("/download/", a.withAuth(a.download))

	server := &http.Server{
		Addr:              a.cfg.AdminListen,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("admin panel listening on http://%s", a.cfg.AdminListen)
	err := server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (a *AdminServer) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		username := a.cfg.AdminUsername
		password := a.cfg.AdminPassword
		a.mu.Unlock()

		user, pass, ok := r.BasicAuth()
		if !ok ||
			subtle.ConstantTimeCompare([]byte(user), []byte(username)) != 1 ||
			subtle.ConstantTimeCompare([]byte(pass), []byte(password)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Solo VPN Admin"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (a *AdminServer) index(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	cfg := *a.cfg
	a.mu.Unlock()

	a.buildMu.Lock()
	buildRunning := a.buildRunning
	lastLog := a.lastLog
	lastAPK := a.lastAPK
	lastWindows := a.lastWindows
	a.buildMu.Unlock()

	hostHint := hostWithoutPort(r.Host)
	if cfg.PublicHost == "" {
		cfg.PublicHost = hostHint
	}

	data := struct {
		Config       Config
		HostHint     string
		BuildRunning bool
		LastLog      string
		LastAPK      string
		LastWindows  string
		Users        []UserAccount
	}{
		Config:       cfg,
		HostHint:     hostHint,
		BuildRunning: buildRunning,
		LastLog:      lastLog,
		LastAPK:      lastAPK,
		LastWindows:  lastWindows,
		Users:        sortedUsers(cfg.Users),
	}

	if err := panelTemplate.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (a *AdminServer) updateUserMinutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	delta := parseInt(r.FormValue("minutes"), 0)
	action := strings.TrimSpace(r.FormValue("action"))
	if action == "subtract" {
		delta = -delta
	}
	if username == "" || delta == 0 {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	a.mu.Lock()
	cfg := *a.cfg
	if cfg.Users == nil {
		cfg.Users = map[string]UserAccount{}
	}
	user, ok := cfg.Users[username]
	if ok {
		user.RemainingMinutes += delta
		if user.RemainingMinutes < 0 {
			user.RemainingMinutes = 0
		}
		cfg.Users[username] = user
		if err := saveConfig(a.cfgPath, &cfg); err != nil {
			a.mu.Unlock()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		*a.cfg = cfg
	}
	a.mu.Unlock()

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *AdminServer) uploadIcon(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(12 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("appIcon")
	if err != nil {
		http.Error(w, "only PNG icons are supported", http.StatusBadRequest)
		return
	}
	defer file.Close()
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".png") {
		http.Error(w, "only PNG icons are supported", http.StatusBadRequest)
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, 10<<20))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(data) < 8 || string(data[:8]) != "\x89PNG\r\n\x1a\n" {
		http.Error(w, "only PNG icons are supported", http.StatusBadRequest)
		return
	}

	target := "/etc/solovpn/app-icon.png"
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.mu.Lock()
	cfg := *a.cfg
	cfg.AppIconUploadPath = target
	if err := saveConfig(a.cfgPath, &cfg); err != nil {
		a.mu.Unlock()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	*a.cfg = cfg
	a.mu.Unlock()

	http.Redirect(w, r, "/#app", http.StatusSeeOther)
}

func (a *AdminServer) apiRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req authRequest
	if err := readJSON(r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad request")
		return
	}
	username, password, ok := normalizeCredentials(req.Username, req.Password)
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "bad request")
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	cfg := *a.cfg
	if cfg.Users == nil {
		cfg.Users = map[string]UserAccount{}
	}
	if _, exists := cfg.Users[username]; exists {
		writeJSONError(w, http.StatusConflict, "account already exists")
		return
	}
	salt, err := randomToken(18)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "server error")
		return
	}
	token, err := randomToken(24)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "server error")
		return
	}
	cfg.Users[username] = UserAccount{
		Username:         username,
		PasswordSalt:     salt,
		PasswordHash:     hashPassword(salt, password),
		Token:            token,
		RemainingMinutes: 0,
	}
	if err := saveConfig(a.cfgPath, &cfg); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "server error")
		return
	}
	*a.cfg = cfg
	writeJSON(w, http.StatusOK, authResponse{Token: token, Username: username, RemainingMinutes: 0})
}

func (a *AdminServer) apiLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req authRequest
	if err := readJSON(r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad request")
		return
	}
	username, password, ok := normalizeCredentials(req.Username, req.Password)
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "bad request")
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	cfg := *a.cfg
	user, exists := cfg.Users[username]
	if !exists || subtle.ConstantTimeCompare([]byte(user.PasswordHash), []byte(hashPassword(user.PasswordSalt, password))) != 1 {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	token, err := randomToken(24)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "server error")
		return
	}
	user.Token = token
	cfg.Users[username] = user
	if err := saveConfig(a.cfgPath, &cfg); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "server error")
		return
	}
	*a.cfg = cfg
	writeJSON(w, http.StatusOK, authResponse{Token: token, Username: username, RemainingMinutes: user.RemainingMinutes})
}

func (a *AdminServer) apiConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	username, token, ok := bearerAuth(r)
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	a.mu.Lock()
	cfg := *a.cfg
	user, ok := cfg.Users[username]
	a.mu.Unlock()
	if !ok || user.Token == "" || subtle.ConstantTimeCompare([]byte(user.Token), []byte(token)) != 1 {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if user.RemainingMinutes <= 0 {
		writeJSONError(w, http.StatusPaymentRequired, "no remaining minutes")
		return
	}

	host := strings.TrimSpace(cfg.PublicHost)
	if host == "" {
		host = hostWithoutPort(r.Host)
	}
	writeJSON(w, http.StatusOK, apiConfigResponse{
		ProfileName:        cfg.AppName,
		ServerHost:         host,
		ServerPort:         cfg.ServerPort,
		ClientAddress:      cfg.ClientAddress,
		ClientPrefixLength: 32,
		DNS:                cfg.DNS,
		MTU:                cfg.MTU,
		SharedKey:          cfg.SharedKey,
		RemainingMinutes:   user.RemainingMinutes,
	})
}

func (a *AdminServer) apiMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	username, token, ok := bearerAuth(r)
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	a.mu.Lock()
	cfg := *a.cfg
	user, exists := cfg.Users[username]
	a.mu.Unlock()
	if !exists || user.Token == "" || subtle.ConstantTimeCompare([]byte(user.Token), []byte(token)) != 1 {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, authResponse{Token: token, Username: username, RemainingMinutes: user.RemainingMinutes})
}

func (a *AdminServer) apiTick(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	username, token, ok := bearerAuth(r)
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	cfg := *a.cfg
	user, exists := cfg.Users[username]
	if !exists || user.Token == "" || subtle.ConstantTimeCompare([]byte(user.Token), []byte(token)) != 1 {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if user.RemainingMinutes > 0 {
		user.RemainingMinutes--
	}
	cfg.Users[username] = user
	if err := saveConfig(a.cfgPath, &cfg); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "server error")
		return
	}
	*a.cfg = cfg
	writeJSON(w, http.StatusOK, authResponse{Token: token, Username: username, RemainingMinutes: user.RemainingMinutes})
}

type authRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type authResponse struct {
	Token            string `json:"token"`
	Username         string `json:"username"`
	RemainingMinutes int    `json:"remainingMinutes"`
}

type apiConfigResponse struct {
	ProfileName        string `json:"profileName"`
	ServerHost         string `json:"serverHost"`
	ServerPort         int    `json:"serverPort"`
	ClientAddress      string `json:"clientAddress"`
	ClientPrefixLength int    `json:"clientPrefixLength"`
	DNS                string `json:"dns"`
	MTU                int    `json:"mtu"`
	SharedKey          string `json:"sharedKey"`
	RemainingMinutes   int    `json:"remainingMinutes"`
}

func readJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func normalizeCredentials(username, password string) (string, string, bool) {
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 32 || len(password) < 6 || len(password) > 72 {
		return "", "", false
	}
	for _, r := range username {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '_' && r != '-' {
			return "", "", false
		}
	}
	return username, password, true
}

func bearerAuth(r *http.Request) (string, string, bool) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(header, "Bearer ") {
		return "", "", false
	}
	raw := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func hashPassword(salt, password string) string {
	sum := sha256.Sum256([]byte(salt + ":" + password))
	return hex.EncodeToString(sum[:])
}

func randomToken(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func sortedUsers(users map[string]UserAccount) []UserAccount {
	list := make([]UserAccount, 0, len(users))
	for _, user := range users {
		list = append(list, user)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Username < list[j].Username
	})
	return list
}

func (a *AdminServer) updateSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	a.mu.Lock()
	cfg := *a.cfg
	if _, ok := r.Form["publicHost"]; ok {
		cfg.PublicHost = strings.TrimSpace(r.FormValue("publicHost"))
	}
	if _, ok := r.Form["serverPort"]; ok {
		cfg.ServerPort = parseInt(r.FormValue("serverPort"), cfg.ServerPort)
		cfg.VPNListen = fmt.Sprintf(":%d", cfg.ServerPort)
	}
	if _, ok := r.Form["clientAddress"]; ok {
		cfg.ClientAddress = strings.TrimSpace(r.FormValue("clientAddress"))
	}
	if _, ok := r.Form["dns"]; ok {
		cfg.DNS = strings.TrimSpace(r.FormValue("dns"))
	}
	if _, ok := r.Form["mtu"]; ok {
		cfg.MTU = parseInt(r.FormValue("mtu"), cfg.MTU)
	}
	if _, ok := r.Form["androidProjectPath"]; ok {
		cfg.AndroidProjectPath = strings.TrimSpace(r.FormValue("androidProjectPath"))
	}
	if _, ok := r.Form["apkOutputDir"]; ok {
		cfg.APKOutputDir = strings.TrimSpace(r.FormValue("apkOutputDir"))
	}
	if _, ok := r.Form["appName"]; ok {
		cfg.AppName = strings.TrimSpace(r.FormValue("appName"))
	}
	if _, ok := r.Form["appPackageName"]; ok {
		cfg.AppPackageName = strings.TrimSpace(r.FormValue("appPackageName"))
	}
	if _, ok := r.Form["appVersionCode"]; ok {
		cfg.AppVersionCode = parseInt(r.FormValue("appVersionCode"), cfg.AppVersionCode)
	}
	if _, ok := r.Form["appVersionName"]; ok {
		cfg.AppVersionName = strings.TrimSpace(r.FormValue("appVersionName"))
	}
	if _, ok := r.Form["appThemePrimary"]; ok {
		cfg.AppThemePrimary = strings.TrimSpace(r.FormValue("appThemePrimary"))
	}
	if _, ok := r.Form["appThemeAccent"]; ok {
		cfg.AppThemeAccent = strings.TrimSpace(r.FormValue("appThemeAccent"))
	}
	if _, ok := r.Form["appIconStartColor"]; ok {
		cfg.AppIconStartColor = strings.TrimSpace(r.FormValue("appIconStartColor"))
	}
	if _, ok := r.Form["appIconEndColor"]; ok {
		cfg.AppIconEndColor = strings.TrimSpace(r.FormValue("appIconEndColor"))
	}
	if _, ok := r.Form["appIconAccentColor"]; ok {
		cfg.AppIconAccentColor = strings.TrimSpace(r.FormValue("appIconAccentColor"))
	}
	if _, ok := r.Form["apkFilePrefix"]; ok {
		cfg.APKFilePrefix = strings.TrimSpace(r.FormValue("apkFilePrefix"))
	}
	if password := strings.TrimSpace(r.FormValue("adminPassword")); password != "" {
		cfg.AdminPassword = password
	}

	if err := cfg.validate(); err != nil {
		a.mu.Unlock()
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := saveConfig(a.cfgPath, &cfg); err != nil {
		a.mu.Unlock()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	*a.cfg = cfg
	a.mu.Unlock()

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *AdminServer) startBuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	a.buildMu.Lock()
	if a.buildRunning {
		a.buildMu.Unlock()
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	a.buildRunning = true
	a.lastLog = "构建已开始...\n"
	a.buildMu.Unlock()

	hostHint := hostWithoutPort(r.Host)
	target := strings.TrimSpace(r.FormValue("target"))
	go func() {
		var logText string
		var artifact string
		var err error
		if target == "windows" {
			logText, artifact, err = a.buildWindowsEXE(context.Background(), hostHint)
		} else {
			logText, artifact, err = a.buildAPK(context.Background(), hostHint)
		}
		if err != nil {
			logText += "\nERROR: " + err.Error() + "\n"
		}

		a.buildMu.Lock()
		a.buildRunning = false
		a.lastLog = logText
		if err == nil {
			if target == "windows" {
				a.lastWindows = artifact
			} else {
				a.lastAPK = artifact
			}
		}
		a.buildMu.Unlock()
	}()

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *AdminServer) buildAPK(ctx context.Context, hostHint string) (string, string, error) {
	a.mu.Lock()
	cfg := *a.cfg
	a.mu.Unlock()

	host := strings.TrimSpace(cfg.PublicHost)
	if host == "" {
		host = hostHint
	}
	if net.ParseIP(host) == nil && strings.Contains(host, ":") {
		return "", "", fmt.Errorf("publicHost should not include a port")
	}

	projectPath := filepath.Clean(cfg.AndroidProjectPath)
	assetPath := filepath.Join(projectPath, "app", "src", "main", "assets", "default_vpn_config.json")
	if _, err := os.Stat(filepath.Join(projectPath, "app", "build.gradle.kts")); err != nil {
		return "", "", fmt.Errorf("android project not found at %s", projectPath)
	}
	if err := applyAndroidBuildSettings(projectPath, cfg); err != nil {
		return "", "", err
	}

	asset := androidAssetConfig{
		APIBaseURL: "http://" + host + adminListenPort(cfg.AdminListen),
	}
	data, err := json.MarshalIndent(asset, "", "  ")
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Dir(assetPath), 0o755); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(assetPath, append(data, '\n'), 0o644); err != nil {
		return "", "", err
	}

	var logBuilder strings.Builder
	logBuilder.WriteString("已写入 APK 内置配置:\n")
	logBuilder.WriteString(string(data))
	logBuilder.WriteString("\n\n正在运行 Gradle 构建...\n")

	chmod := exec.CommandContext(ctx, "chmod", "+x", "./gradlew")
	chmod.Dir = projectPath
	if output, err := chmod.CombinedOutput(); err != nil {
		logBuilder.Write(output)
		return logBuilder.String(), "", err
	}

	cmd := exec.CommandContext(ctx, "./gradlew", ":app:assembleDebug", "--no-daemon")
	cmd.Dir = projectPath
	output, err := cmd.CombinedOutput()
	logBuilder.Write(output)
	if err != nil {
		return logBuilder.String(), "", err
	}

	sourceAPK := filepath.Join(projectPath, "app", "build", "outputs", "apk", "debug", "app-debug.apk")
	if err := os.MkdirAll(cfg.APKOutputDir, 0o755); err != nil {
		return logBuilder.String(), "", err
	}
	fileName := sanitizeFilePrefix(cfg.APKFilePrefix) + "-" + time.Now().Format("20060102-150405") + ".apk"
	targetAPK := filepath.Join(cfg.APKOutputDir, fileName)
	if err := copyFile(sourceAPK, targetAPK); err != nil {
		return logBuilder.String(), "", err
	}

	logBuilder.WriteString("\nAPK 构建完成: " + targetAPK + "\n")
	return logBuilder.String(), fileName, nil
}

func (a *AdminServer) buildWindowsEXE(ctx context.Context, hostHint string) (string, string, error) {
	a.mu.Lock()
	cfg := *a.cfg
	a.mu.Unlock()

	host := strings.TrimSpace(cfg.PublicHost)
	if host == "" {
		host = hostHint
	}
	if net.ParseIP(host) == nil && strings.Contains(host, ":") {
		return "", "", fmt.Errorf("publicHost should not include a port")
	}

	projectPath := filepath.Clean(cfg.AndroidProjectPath)
	clientPath := filepath.Join(projectPath, "client", "windows")
	if _, err := os.Stat(filepath.Join(clientPath, "go.mod")); err != nil {
		return "", "", fmt.Errorf("windows client project not found at %s", clientPath)
	}
	if err := os.MkdirAll(cfg.APKOutputDir, 0o755); err != nil {
		return "", "", err
	}
	cacheRoot := filepath.Join(filepath.Dir(cfg.APKOutputDir), "go-cache")
	modCacheRoot := filepath.Join(filepath.Dir(cfg.APKOutputDir), "go-mod-cache")
	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(modCacheRoot, 0o755); err != nil {
		return "", "", err
	}

	fileName := sanitizeFilePrefix(cfg.APKFilePrefix) + "-windows-" + time.Now().Format("20060102-150405") + ".exe"
	targetEXE := filepath.Join(cfg.APKOutputDir, fileName)
	apiBaseURL := "http://" + host + adminListenPort(cfg.AdminListen)

	var logBuilder strings.Builder
	logBuilder.WriteString("正在构建 Windows 客户端 EXE...\n")
	logBuilder.WriteString("内置 API 地址: " + apiBaseURL + "\n\n")

	cmd := exec.CommandContext(
		ctx,
		"go",
		"build",
		"-trimpath",
		"-ldflags",
		"-s -w -H windowsgui -X main.defaultAPIBaseURL="+apiBaseURL,
		"-o",
		targetEXE,
		".",
	)
	cmd.Dir = clientPath
	cmd.Env = append(
		os.Environ(),
		"GOOS=windows",
		"GOARCH=amd64",
		"CGO_ENABLED=0",
		"HOME="+filepath.Dir(cfg.APKOutputDir),
		"GOCACHE="+cacheRoot,
		"GOMODCACHE="+modCacheRoot,
	)
	output, err := cmd.CombinedOutput()
	logBuilder.Write(output)
	if err != nil {
		return logBuilder.String(), "", err
	}

	logBuilder.WriteString("\nWindows EXE 构建完成: " + targetEXE + "\n")
	logBuilder.WriteString("运行示例: solovpn-client.exe -username USER -password PASS\n")
	return logBuilder.String(), fileName, nil
}

func (a *AdminServer) download(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/download/")
	if name == "" || strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
		http.Error(w, "bad file name", http.StatusBadRequest)
		return
	}

	a.mu.Lock()
	path := filepath.Join(a.cfg.APKOutputDir, name)
	a.mu.Unlock()
	http.ServeFile(w, r, path)
}

func copyFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(target)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func parseInt(raw string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return parsed
}

func hostWithoutPort(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return strings.Trim(host, "[]")
}

func applyAndroidBuildSettings(projectPath string, cfg Config) error {
	stringsPath := filepath.Join(projectPath, "app", "src", "main", "res", "values", "strings.xml")
	stringsXML := fmt.Sprintf("<resources>\n    <string name=\"app_name\">%s</string>\n</resources>\n", template.HTMLEscapeString(cfg.AppName))
	if err := os.WriteFile(stringsPath, []byte(stringsXML), 0o644); err != nil {
		return err
	}

	gradlePath := filepath.Join(projectPath, "app", "build.gradle.kts")
	gradleRaw, err := os.ReadFile(gradlePath)
	if err != nil {
		return err
	}
	gradle := string(gradleRaw)
	gradle = regexp.MustCompile(`applicationId\s*=\s*"[^"]+"`).ReplaceAllString(gradle, fmt.Sprintf(`applicationId = "%s"`, cfg.AppPackageName))
	gradle = regexp.MustCompile(`versionCode\s*=\s*\d+`).ReplaceAllString(gradle, fmt.Sprintf(`versionCode = %d`, cfg.AppVersionCode))
	gradle = regexp.MustCompile(`versionName\s*=\s*"[^"]+"`).ReplaceAllString(gradle, fmt.Sprintf(`versionName = "%s"`, cfg.AppVersionName))
	if err := os.WriteFile(gradlePath, []byte(gradle), 0o644); err != nil {
		return err
	}

	colorsPath := filepath.Join(projectPath, "app", "src", "main", "res", "values", "colors.xml")
	colorsRaw, err := os.ReadFile(colorsPath)
	if err != nil {
		return err
	}
	colors := string(colorsRaw)
	colors = replaceColor(colors, "solo_primary", strings.ToUpper(cfg.AppThemePrimary))
	colors = replaceColor(colors, "solo_primary_dark", darkenHexColor(cfg.AppThemePrimary))
	colors = replaceColor(colors, "solo_accent", strings.ToUpper(cfg.AppThemeAccent))
	if err := os.WriteFile(colorsPath, []byte(colors), 0o644); err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join(projectPath, "app", "src", "main", "res", "drawable", "ic_launcher_background.xml"), []byte(iconBackgroundXML(cfg)), 0o644); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.AppIconUploadPath) != "" {
		if err := applyUploadedIcon(projectPath, cfg.AppIconUploadPath); err != nil {
			return err
		}
	} else {
		if err := os.WriteFile(filepath.Join(projectPath, "app", "src", "main", "res", "drawable", "ic_launcher_foreground.xml"), []byte(iconForegroundXML(cfg)), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func applyUploadedIcon(projectPath, source string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read uploaded icon: %w", err)
	}
	if len(data) < 8 || string(data[:8]) != "\x89PNG\r\n\x1a\n" {
		return fmt.Errorf("uploaded icon is not a valid PNG")
	}
	drawablePath := filepath.Join(projectPath, "app", "src", "main", "res", "drawable", "uploaded_launcher_icon.png")
	if err := os.WriteFile(drawablePath, data, 0o644); err != nil {
		return err
	}
	for _, dir := range []string{"mipmap-mdpi", "mipmap-hdpi", "mipmap-xhdpi", "mipmap-xxhdpi", "mipmap-xxxhdpi"} {
		base := filepath.Join(projectPath, "app", "src", "main", "res", dir)
		_ = os.MkdirAll(base, 0o755)
		if err := os.WriteFile(filepath.Join(base, "ic_launcher.png"), data, 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(base, "ic_launcher_round.png"), data, 0o644); err != nil {
			return err
		}
	}
	adaptive := `<?xml version="1.0" encoding="utf-8"?>
<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">
    <background android:drawable="@drawable/ic_launcher_background" />
    <foreground android:drawable="@drawable/uploaded_launcher_icon" />
    <monochrome android:drawable="@drawable/uploaded_launcher_icon" />
</adaptive-icon>
`
	for _, name := range []string{"ic_launcher.xml", "ic_launcher_round.xml"} {
		if err := os.WriteFile(filepath.Join(projectPath, "app", "src", "main", "res", "mipmap-anydpi-v26", name), []byte(adaptive), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func replaceColor(raw, name, color string) string {
	re := regexp.MustCompile(`(<color name="` + regexp.QuoteMeta(name) + `">)#[0-9A-Fa-f]{6,8}(</color>)`)
	return re.ReplaceAllString(raw, "${1}"+color+"${2}")
}

func darkenHexColor(color string) string {
	color = strings.TrimPrefix(color, "#")
	if len(color) != 6 {
		return "#0B4FC7"
	}
	r, _ := strconv.ParseInt(color[0:2], 16, 64)
	g, _ := strconv.ParseInt(color[2:4], 16, 64)
	b, _ := strconv.ParseInt(color[4:6], 16, 64)
	return fmt.Sprintf("#%02X%02X%02X", int(float64(r)*0.72), int(float64(g)*0.72), int(float64(b)*0.72))
}

func iconBackgroundXML(cfg Config) string {
	start := "#FF" + strings.TrimPrefix(strings.ToUpper(cfg.AppIconStartColor), "#")
	end := "#FF" + strings.TrimPrefix(strings.ToUpper(cfg.AppIconEndColor), "#")
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<vector xmlns:android="http://schemas.android.com/apk/res/android"
    xmlns:aapt="http://schemas.android.com/aapt"
    android:width="108dp"
    android:height="108dp"
    android:viewportWidth="108"
    android:viewportHeight="108">
    <path android:pathData="M0,0h108v108h-108z">
        <aapt:attr name="android:fillColor">
            <gradient
                android:type="linear"
                android:startX="14"
                android:startY="8"
                android:endX="96"
                android:endY="102">
                <item android:offset="0" android:color="%s" />
                <item android:offset="1" android:color="%s" />
            </gradient>
        </aapt:attr>
    </path>
    <path android:fillColor="#38FFFFFF" android:pathData="M13,21m-6,0a6,6 0,1 0,12 0a6,6 0,1 0,-12 0" />
    <path android:fillColor="#26FFFFFF" android:pathData="M92,22m-9,0a9,9 0,1 0,18 0a9,9 0,1 0,-18 0" />
    <path android:fillColor="#30FFFFFF" android:pathData="M18,82m-10,0a10,10 0,1 0,20 0a10,10 0,1 0,-20 0" />
    <path android:fillColor="#26FFFFFF" android:pathData="M7,55C26,42 42,43 55,53C68,63 81,62 101,47L101,70C82,83 64,83 51,73C38,63 25,65 7,78Z" />
</vector>
`, start, end)
}

func iconForegroundXML(cfg Config) string {
	accent := "#FF" + strings.TrimPrefix(strings.ToUpper(cfg.AppIconAccentColor), "#")
	stroke := "#FF" + strings.TrimPrefix(strings.ToUpper(darkenHexColor(cfg.AppIconAccentColor)), "#")
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<vector xmlns:android="http://schemas.android.com/apk/res/android"
    android:width="108dp"
    android:height="108dp"
    android:viewportWidth="108"
    android:viewportHeight="108">
    <path android:fillColor="#26000000" android:pathData="M31,77C22,77 15,70 15,61C15,53 21,46 29,45C32,35 42,28 54,28C67,28 78,37 80,49C88,51 94,57 94,66C94,75 87,82 78,82H32C31.7,82 31.3,82 31,82Z" />
    <path android:fillColor="#FFFFFFFF" android:pathData="M31,73C23,73 17,67 17,59C17,52 23,46 30,46C33,36 42,30 53,30C65,30 75,38 78,50C86,51 91,57 91,65C91,73 85,79 77,79H31Z" />
    <path android:fillColor="%s" android:pathData="M37,61C37,57 41,53 45,53H63C67,53 71,57 71,61V72C71,76 67,80 63,80H45C41,80 37,76 37,72Z" />
    <path android:fillColor="#00000000" android:pathData="M45,53V48C45,43 49,39 54,39C59,39 63,43 63,48V53" android:strokeColor="%s" android:strokeLineCap="round" android:strokeWidth="5" />
    <path android:fillColor="#FFFFFFFF" android:pathData="M49,66m-3,0a3,3 0,1 0,6 0a3,3 0,1 0,-6 0" />
    <path android:fillColor="#FFFFFFFF" android:pathData="M59,66m-3,0a3,3 0,1 0,6 0a3,3 0,1 0,-6 0" />
    <path android:fillColor="#00000000" android:pathData="M49,71C51,73 57,73 59,71" android:strokeColor="#FFFFFFFF" android:strokeLineCap="round" android:strokeWidth="2.5" />
    <path android:fillColor="#FFFFFFFF" android:pathData="M35,44L38,39L41,44L46,47L41,50L38,55L35,50L30,47Z" />
    <path android:fillColor="#B3FFFFFF" android:pathData="M76,34L78,31L80,34L83,36L80,38L78,41L76,38L73,36Z" />
</vector>
`, accent, stroke)
}

func sanitizeFilePrefix(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "solovpn"
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "solovpn"
	}
	return b.String()
}

func adminListenPort(listen string) string {
	if _, port, err := net.SplitHostPort(listen); err == nil {
		return ":" + port
	}
	if strings.HasPrefix(listen, ":") {
		return listen
	}
	return ":8080"
}

var panelTemplate = template.Must(template.New("panel").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>极速传 VPN 管理面板</title>
  <style>
    :root { --ink: #14213d; --muted: #667085; --line: #dce5f2; --panel: rgba(255,255,255,.9); --blue: #126cff; --green: #16a085; --dark: #111827; }
    * { box-sizing: border-box; }
    body { margin: 0; min-height: 100vh; font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; background: linear-gradient(160deg, #f5f8ff 0%, #eaf4f1 55%, #f8fbff 100%); color: var(--ink); }
    main { max-width: 1120px; margin: 0 auto; padding: 34px 18px 60px; }
    header { display: grid; grid-template-columns: 1fr auto; gap: 18px; align-items: end; margin-bottom: 18px; animation: rise .42s ease both; }
    h1 { margin: 0; font-size: clamp(30px, 5vw, 48px); line-height: 1; letter-spacing: 0; }
    h2 { margin: 0 0 4px; font-size: 20px; }
    p { margin: 10px 0 0; color: var(--muted); max-width: 720px; line-height: 1.55; }
    section { background: var(--panel); border: 1px solid rgba(220,229,242,.92); border-radius: 8px; box-shadow: 0 18px 48px rgba(20,33,61,.09); padding: 22px; margin-top: 16px; backdrop-filter: blur(14px); animation: rise .5s ease both; scroll-margin-top: 16px; }
    label { display: block; font-size: 12px; color: var(--muted); font-weight: 700; letter-spacing: .02em; margin: 14px 0 7px; text-transform: uppercase; }
    input { width: 100%; border: 1px solid var(--line); border-radius: 8px; background: #f8fafc; color: var(--ink); padding: 12px 13px; font-size: 15px; outline: none; transition: border-color .18s ease, box-shadow .18s ease, background .18s ease; }
    input:focus { background: #fff; border-color: var(--blue); box-shadow: 0 0 0 4px rgba(18,108,255,.12); }
    input[type="color"] { height: 46px; padding: 5px; cursor: pointer; }
    input[type="file"] { padding: 10px; background: #fff; }
    button, a.button { display: inline-flex; align-items: center; justify-content: center; min-height: 42px; border: 0; border-radius: 8px; background: linear-gradient(135deg, var(--blue), var(--green)); color: #fff; padding: 11px 16px; font-size: 15px; font-weight: 800; text-decoration: none; cursor: pointer; transition: transform .16s ease, box-shadow .16s ease, opacity .16s ease; box-shadow: 0 10px 24px rgba(18,108,255,.22); }
    button:hover, a.button:hover, .tabs a:hover { transform: translateY(-1px); }
    button:active, a.button:active { transform: translateY(0) scale(.98); }
    button:disabled { opacity: .55; cursor: wait; }
    button.secondary, a.secondary { background: #14213d; box-shadow: 0 10px 24px rgba(20,33,61,.2); }
    pre { overflow: auto; background: #111827; color: #d7e3f4; border: 1px solid #263244; border-radius: 8px; padding: 16px; min-height: 132px; line-height: 1.5; }
    table { width: 100%; border-collapse: collapse; margin-top: 14px; overflow: hidden; border-radius: 8px; }
    th, td { border-bottom: 1px solid var(--line); padding: 12px 10px; text-align: left; font-size: 14px; }
    th { color: var(--muted); font-size: 12px; text-transform: uppercase; letter-spacing: .02em; }
    td form { display: inline-flex; gap: 8px; align-items: center; flex-wrap: wrap; }
    td input { width: 120px; }
    .tiny { min-height: 36px; padding: 8px 12px; font-size: 13px; box-shadow: none; }
    .grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 18px; }
    .muted { color: var(--muted); font-size: 13px; }
    .row { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; margin-top: 18px; }
    .badge { justify-self: end; border: 1px solid rgba(22,160,133,.2); background: #e8f7f2; color: #0f7e69; border-radius: 999px; padding: 9px 13px; font-weight: 800; font-size: 13px; }
    .section-head { display: flex; justify-content: space-between; gap: 14px; align-items: center; margin-bottom: 4px; }
    .pulse { width: 9px; height: 9px; border-radius: 50%; background: var(--green); display: inline-block; margin-right: 8px; box-shadow: 0 0 0 rgba(22,160,133,.5); animation: pulse 1.8s infinite; }
    .tabs { position: sticky; top: 0; z-index: 5; display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 8px; padding: 8px; margin: 0 0 18px; background: rgba(245,248,255,.86); border: 1px solid rgba(220,229,242,.9); border-radius: 8px; backdrop-filter: blur(14px); }
    .tabs a { min-height: 42px; display: inline-flex; align-items: center; justify-content: center; border-radius: 8px; color: var(--ink); background: #fff; border: 1px solid var(--line); text-decoration: none; font-weight: 800; transition: transform .16s ease, border-color .16s ease; }
    .tabs a:focus, .tabs a:hover { border-color: var(--blue); }
    .color-row { display: grid; grid-template-columns: 64px 1fr; gap: 10px; align-items: center; }
    .file-note { margin-top: 8px; color: var(--muted); font-size: 13px; line-height: 1.5; }
    @keyframes rise { from { opacity: 0; transform: translateY(16px); } to { opacity: 1; transform: translateY(0); } }
    @keyframes pulse { 0% { box-shadow: 0 0 0 0 rgba(22,160,133,.5); } 70% { box-shadow: 0 0 0 10px rgba(22,160,133,0); } 100% { box-shadow: 0 0 0 0 rgba(22,160,133,0); } }
    @media (max-width: 760px) { main { padding-top: 22px; } header, .grid, .tabs { grid-template-columns: 1fr; } .badge { justify-self: start; } section { padding: 18px; } }
  </style>
</head>
<body>
<main>
  <header>
    <div>
      <h1>极速传 VPN 管理面板</h1>
      <p>分区管理服务器、App 构建、用户时长和 APK 发布。App 连接配置仍由服务端登录后下发。</p>
    </div>
    <div class="badge"><span class="pulse"></span>面板在线</div>
  </header>

  <nav class="tabs" aria-label="管理分区">
    <a href="#server">服务器设置</a>
    <a href="#app">App 构建</a>
    <a href="#users">用户时长</a>
    <a href="#build">构建下载</a>
  </nav>

  <section id="server">
    <div class="section-head">
      <div>
        <h2>服务器设置</h2>
        <span class="muted">公网地址、VPN 端口和服务端运行参数。</span>
      </div>
    </div>
    <form method="post" action="/settings">
      <div class="grid">
        <div>
          <label>公网服务器地址</label>
          <input name="publicHost" value="{{.Config.PublicHost}}" placeholder="{{.HostHint}}">
        </div>
        <div>
          <label>VPN UDP 端口</label>
          <input name="serverPort" value="{{.Config.ServerPort}}" inputmode="numeric">
        </div>
        <div>
          <label>客户端虚拟 IP</label>
          <input name="clientAddress" value="{{.Config.ClientAddress}}">
        </div>
        <div>
          <label>DNS 服务器</label>
          <input name="dns" value="{{.Config.DNS}}">
        </div>
        <div>
          <label>MTU 数值</label>
          <input name="mtu" value="{{.Config.MTU}}" inputmode="numeric">
        </div>
        <div>
          <label>修改管理密码</label>
          <input name="adminPassword" value="" placeholder="留空表示保持当前密码">
        </div>
        <div>
          <label>Android 项目路径</label>
          <input name="androidProjectPath" value="{{.Config.AndroidProjectPath}}">
        </div>
        <div>
          <label>APK 输出目录</label>
          <input name="apkOutputDir" value="{{.Config.APKOutputDir}}">
        </div>
      </div>
      <div class="row">
        <button type="submit">保存服务器设置</button>
      </div>
    </form>
  </section>

  <section id="app">
    <div class="section-head">
      <div>
        <h2>App 构建设置</h2>
        <span class="muted">应用名称、包名、版本、颜色和图标会在构建 APK 前自动写入 Android 项目。</span>
      </div>
    </div>
    <form method="post" action="/settings">
      <div class="grid">
        <div>
          <label>App 名称</label>
          <input name="appName" value="{{.Config.AppName}}">
        </div>
        <div>
          <label>App 包名</label>
          <input name="appPackageName" value="{{.Config.AppPackageName}}" placeholder="com.example.app">
        </div>
        <div>
          <label>版本号</label>
          <input name="appVersionCode" value="{{.Config.AppVersionCode}}" inputmode="numeric">
        </div>
        <div>
          <label>版本名称</label>
          <input name="appVersionName" value="{{.Config.AppVersionName}}">
        </div>
        <div>
          <label>主题主色</label>
          <div class="color-row"><input type="color" name="appThemePrimary" value="{{.Config.AppThemePrimary}}"><input value="{{.Config.AppThemePrimary}}" readonly></div>
        </div>
        <div>
          <label>主题强调色</label>
          <div class="color-row"><input type="color" name="appThemeAccent" value="{{.Config.AppThemeAccent}}"><input value="{{.Config.AppThemeAccent}}" readonly></div>
        </div>
        <div>
          <label>图标渐变起始色</label>
          <div class="color-row"><input type="color" name="appIconStartColor" value="{{.Config.AppIconStartColor}}"><input value="{{.Config.AppIconStartColor}}" readonly></div>
        </div>
        <div>
          <label>图标渐变结束色</label>
          <div class="color-row"><input type="color" name="appIconEndColor" value="{{.Config.AppIconEndColor}}"><input value="{{.Config.AppIconEndColor}}" readonly></div>
        </div>
        <div>
          <label>图标主体色</label>
          <div class="color-row"><input type="color" name="appIconAccentColor" value="{{.Config.AppIconAccentColor}}"><input value="{{.Config.AppIconAccentColor}}" readonly></div>
        </div>
        <div>
          <label>APK 文件名前缀</label>
          <input name="apkFilePrefix" value="{{.Config.APKFilePrefix}}" placeholder="solovpn">
        </div>
      </div>
      <div class="row">
        <button type="submit">保存 App 设置</button>
        <span class="muted">如果上传了 PNG 图标，构建时会优先使用上传图标；未上传时使用上面的图标颜色生成矢量图标。</span>
      </div>
    </form>
    <form method="post" action="/icon" enctype="multipart/form-data">
      <label>上传 App 图标 PNG</label>
      <input type="file" name="appIcon" accept="image/png">
      <div class="file-note">建议上传 512x512 或 1024x1024 的正方形 PNG。当前图标路径：{{if .Config.AppIconUploadPath}}{{.Config.AppIconUploadPath}}{{else}}未上传，使用自动生成图标{{end}}</div>
      <div class="row">
        <button class="secondary" type="submit">上传图标</button>
      </div>
    </form>
  </section>

  <section id="users">
    <div class="section-head">
      <div>
        <h2>用户时长管理</h2>
        <span class="muted">新注册用户默认为 0 分钟，可在这里增加或扣减连接时长。</span>
      </div>
    </div>
    <table>
      <thead>
        <tr>
          <th>账号</th>
          <th>剩余时长</th>
          <th>调整</th>
        </tr>
      </thead>
      <tbody>
        {{range .Users}}
        <tr>
          <td>{{.Username}}</td>
          <td>{{.RemainingMinutes}} 分钟</td>
          <td>
            <form method="post" action="/users">
              <input type="hidden" name="username" value="{{.Username}}">
              <input name="minutes" value="60" inputmode="numeric">
              <button class="tiny" type="submit" name="action" value="add">增加</button>
              <button class="tiny secondary" type="submit" name="action" value="subtract">扣减</button>
            </form>
          </td>
        </tr>
        {{else}}
        <tr>
          <td colspan="3" class="muted">还没有注册用户。</td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </section>

  <section id="build">
    <div class="section-head">
      <div>
        <h2>构建下载</h2>
        <span class="muted">构建 Android APK 或 Windows EXE，并发布到下载目录。</span>
      </div>
    </div>
    <form method="post" action="/build">
      <div class="row">
        <button class="secondary" type="submit" name="target" value="apk" {{if .BuildRunning}}disabled{{end}}>在服务器上构建 APK</button>
        <button class="secondary" type="submit" name="target" value="windows" {{if .BuildRunning}}disabled{{end}}>构建 Windows EXE</button>
        {{if .BuildRunning}}<span>正在构建，请稍后刷新。</span>{{end}}
        {{if .LastAPK}}<a class="button" href="/download/{{.LastAPK}}">下载最新 APK</a>{{end}}
        {{if .LastWindows}}<a class="button" href="/download/{{.LastWindows}}">下载最新 Windows EXE</a>{{end}}
      </div>
    </form>
    <pre>{{.LastLog}}</pre>
  </section>
</main>
</body>
</html>`))
