package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	guiWidth  = 430
	guiHeight = 360

	idAPI      = 101
	idUser     = 102
	idPassword = 103
	idLogin    = 201
	idRegister = 202
	idConnect  = 203
	idStop     = 204
	idStatus   = 301
	idAccount  = 302
	idTime     = 303

	wmCreate  = 0x0001
	wmDestroy = 0x0002
	wmCommand = 0x0111
	wmApp     = 0x8000

	wsOverlapped  = 0x00000000
	wsCaption     = 0x00C00000
	wsSysMenu     = 0x00080000
	wsMinimizeBox = 0x00020000
	wsVisible     = 0x10000000
	wsChild       = 0x40000000
	wsTabStop     = 0x00010000
	wsBorder      = 0x00800000
	esPassword    = 0x0020

	bsPushButton = 0x00000000

	swShow = 5
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")

	procCreateWindowEx   = user32.NewProc("CreateWindowExW")
	procDefWindowProc    = user32.NewProc("DefWindowProcW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procDispatchMessage  = user32.NewProc("DispatchMessageW")
	procGetMessage       = user32.NewProc("GetMessageW")
	procGetModuleHandle  = kernel32.NewProc("GetModuleHandleW")
	procGetWindowText    = user32.NewProc("GetWindowTextW")
	procLoadCursor       = user32.NewProc("LoadCursorW")
	procPostMessage      = user32.NewProc("PostMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procRegisterClassEx  = user32.NewProc("RegisterClassExW")
	procSendMessage      = user32.NewProc("SendMessageW")
	procSetWindowText    = user32.NewProc("SetWindowTextW")
	procShowWindow       = user32.NewProc("ShowWindow")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procUpdateWindow     = user32.NewProc("UpdateWindow")
	procGetStockObject   = gdi32.NewProc("GetStockObject")
	procEnableWindow     = user32.NewProc("EnableWindow")
)

type point struct {
	X int32
	Y int32
}

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type guiApp struct {
	hwnd       uintptr
	api        uintptr
	user       uintptr
	password   uintptr
	login      uintptr
	register   uintptr
	connect    uintptr
	stop       uintptr
	status     uintptr
	account    uintptr
	time       uintptr
	session    *authSession
	cfg        *Config
	apiBaseURL string
	cancel     context.CancelFunc
	refresh    context.CancelFunc
	connected  bool
	mu         sync.Mutex
}

var app *guiApp

func runGUI(defaultAPI string) {
	app = &guiApp{apiBaseURL: strings.TrimRight(strings.TrimSpace(defaultAPI), "/")}

	instance, _, _ := procGetModuleHandle.Call(0)
	className := utf16Ptr("SoloVPNWindow")
	cursor, _, _ := procLoadCursor.Call(0, uintptr(32512))
	background, _, _ := procGetStockObject.Call(0)
	wc := wndClassEx{
		Size:       uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:    syscall.NewCallback(windowProc),
		Instance:   instance,
		Cursor:     cursor,
		Background: background + 1,
		ClassName:  className,
	}
	procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))

	hwnd := createWindow(0, "SoloVPNWindow", wsOverlapped|wsCaption|wsSysMenu|wsMinimizeBox|wsVisible, 0, 0, guiWidth, guiHeight, 0, 0, instance, "Solo VPN")
	app.hwnd = hwnd
	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)

	var m msg
	for {
		ret, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func windowProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmCreate:
		app.hwnd = hwnd
		app.createControls()
		return 0
	case wmCommand:
		app.handleCommand(int(wParam & 0xffff))
		return 0
	case wmApp:
		app.render()
		return 0
	case wmDestroy:
		app.stopRefresh()
		app.stopTunnel()
		procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
	return ret
}

func (g *guiApp) createControls() {
	x := 28
	y := 22
	createLabel(g.hwnd, "服务器 API", x, y, 100, 22)
	g.api = createEdit(g.hwnd, idAPI, firstNonBlank(g.apiBaseURL, "http://YOUR_SERVER_IP:8080"), x+105, y-2, 260, 24, 0)
	y += 42
	createLabel(g.hwnd, "账号", x, y, 100, 22)
	g.user = createEdit(g.hwnd, idUser, "", x+105, y-2, 260, 24, 0)
	y += 42
	createLabel(g.hwnd, "密码", x, y, 100, 22)
	g.password = createEdit(g.hwnd, idPassword, "", x+105, y-2, 260, 24, esPassword)
	y += 44
	g.login = createButton(g.hwnd, idLogin, "登录", x+105, y, 78, 30)
	g.register = createButton(g.hwnd, idRegister, "注册", x+192, y, 78, 30)
	y += 48
	g.account = createLabel(g.hwnd, "当前账号：未登录", x, y, 360, 22)
	y += 28
	g.time = createLabel(g.hwnd, "剩余时长：-", x, y, 360, 22)
	y += 38
	g.connect = createButton(g.hwnd, idConnect, "连接 VPN", x, y, 120, 34)
	g.stop = createButton(g.hwnd, idStop, "断开", x+132, y, 90, 34)
	y += 54
	g.status = createLabel(g.hwnd, "状态：请先登录", x, y, 360, 44)
	g.render()
}

func (g *guiApp) handleCommand(id int) {
	switch id {
	case idLogin:
		g.auth(false)
	case idRegister:
		g.auth(true)
	case idConnect:
		g.connectTunnel()
	case idStop:
		g.stopTunnel()
		g.setStatus("状态：已断开")
	}
}

func (g *guiApp) auth(registerAccount bool) {
	api := strings.TrimRight(strings.TrimSpace(getText(g.api)), "/")
	username := strings.TrimSpace(getText(g.user))
	password := getText(g.password)
	if api == "" || username == "" || password == "" {
		g.setStatus("状态：请填写服务器、账号和密码")
		return
	}
	g.setBusy(true)
	g.setStatus("状态：正在请求服务器...")
	go func() {
		var session *authSession
		var err error
		if registerAccount {
			session, err = register(api, username, password)
		} else {
			session, err = login(api, username, password)
		}
		if err == nil && session.RemainingMinutes > 0 {
			var cfg *Config
			var remaining int
			cfg, remaining, err = fetchConfig(api, session)
			if err == nil {
				session.RemainingMinutes = remaining
				g.mu.Lock()
				g.apiBaseURL = api
				g.session = session
				g.cfg = cfg
				g.mu.Unlock()
			}
		} else if err == nil {
			g.mu.Lock()
			g.apiBaseURL = api
			g.session = session
			g.cfg = nil
			g.mu.Unlock()
		}
		if err != nil {
			g.setStatus("状态：" + err.Error())
		} else if registerAccount {
			g.setStatus("状态：注册成功")
			g.startRefresh()
		} else {
			g.setStatus("状态：登录成功")
			g.startRefresh()
		}
		g.setBusy(false)
		g.postRender()
	}()
}

func (g *guiApp) connectTunnel() {
	g.mu.Lock()
	if g.connected {
		g.mu.Unlock()
		g.setStatus("状态：VPN 已连接")
		return
	}
	cfg := g.cfg
	session := g.session
	api := g.apiBaseURL
	g.mu.Unlock()
	if cfg == nil || session == nil {
		if session != nil && session.RemainingMinutes <= 0 {
			g.setStatus("状态：连接时长不足")
		} else {
			g.setStatus("状态：请先登录")
		}
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	g.mu.Lock()
	g.cancel = cancel
	g.connected = true
	g.mu.Unlock()
	g.setStatus("状态：VPN 正在连接...")
	g.postRender()
	go func() {
		err := runTunnel(ctx, cfg, api, session)
		g.mu.Lock()
		g.connected = false
		g.cancel = nil
		g.mu.Unlock()
		if err != nil && ctx.Err() == nil {
			g.setStatus("状态：" + err.Error())
		} else {
			g.setStatus("状态：已断开")
		}
		g.postRender()
	}()
}

func (g *guiApp) stopTunnel() {
	g.mu.Lock()
	cancel := g.cancel
	g.cancel = nil
	g.connected = false
	g.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	g.postRender()
}

func (g *guiApp) stopRefresh() {
	g.mu.Lock()
	cancel := g.refresh
	g.refresh = nil
	g.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (g *guiApp) startRefresh() {
	g.mu.Lock()
	if g.refresh != nil {
		g.refresh()
	}
	ctx, cancel := context.WithCancel(context.Background())
	g.refresh = cancel
	g.mu.Unlock()

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				g.refreshAccount()
			}
		}
	}()
}

func (g *guiApp) refreshAccount() {
	g.mu.Lock()
	api := g.apiBaseURL
	session := g.session
	cfg := g.cfg
	g.mu.Unlock()
	if api == "" || session == nil {
		return
	}
	next, err := me(api, session)
	if err != nil {
		return
	}
	g.mu.Lock()
	g.session = next
	g.mu.Unlock()
	if cfg == nil && next.RemainingMinutes > 0 {
		if freshCfg, remaining, err := fetchConfig(api, next); err == nil {
			next.RemainingMinutes = remaining
			g.mu.Lock()
			g.session = next
			g.cfg = freshCfg
			g.mu.Unlock()
		}
	}
	g.postRender()
}

func (g *guiApp) render() {
	g.mu.Lock()
	session := g.session
	connected := g.connected
	g.mu.Unlock()
	if session == nil {
		setText(g.account, "当前账号：未登录")
		setText(g.time, "剩余时长：-")
		enable(g.connect, false)
		enable(g.stop, false)
		return
	}
	setText(g.account, "当前账号："+session.Username)
	setText(g.time, fmt.Sprintf("剩余时长：%d 分钟", session.RemainingMinutes))
	enable(g.connect, !connected && session.RemainingMinutes > 0)
	enable(g.stop, connected)
}

func (g *guiApp) setBusy(busy bool) {
	enable(g.login, !busy)
	enable(g.register, !busy)
}

func (g *guiApp) setStatus(text string) {
	setText(g.status, text)
}

func (g *guiApp) postRender() {
	procPostMessage.Call(g.hwnd, wmApp, 0, 0)
}

func createLabel(parent uintptr, text string, x, y, width, height int) uintptr {
	return createWindow(0, "STATIC", wsChild|wsVisible, x, y, width, height, parent, 0, 0, text)
}

func createEdit(parent uintptr, id int, text string, x, y, width, height int, extra uintptr) uintptr {
	return createWindow(0, "EDIT", wsChild|wsVisible|wsBorder|wsTabStop|extra, x, y, width, height, parent, uintptr(id), 0, text)
}

func createButton(parent uintptr, id int, text string, x, y, width, height int) uintptr {
	return createWindow(0, "BUTTON", wsChild|wsVisible|wsTabStop|bsPushButton, x, y, width, height, parent, uintptr(id), 0, text)
}

func createWindow(exStyle uintptr, className string, style uintptr, x, y, width, height int, parent, menu, instance uintptr, text ...string) uintptr {
	title := ""
	if len(text) > 0 {
		title = text[0]
	}
	hwnd, _, _ := procCreateWindowEx.Call(
		exStyle,
		uintptr(unsafe.Pointer(utf16Ptr(className))),
		uintptr(unsafe.Pointer(utf16Ptr(title))),
		style,
		uintptr(int32(x)),
		uintptr(int32(y)),
		uintptr(int32(width)),
		uintptr(int32(height)),
		parent,
		menu,
		instance,
		0,
	)
	return hwnd
}

func getText(hwnd uintptr) string {
	buf := make([]uint16, 1024)
	procGetWindowText.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

func setText(hwnd uintptr, text string) {
	procSetWindowText.Call(hwnd, uintptr(unsafe.Pointer(utf16Ptr(text))))
}

func enable(hwnd uintptr, enabled bool) {
	value := uintptr(0)
	if enabled {
		value = 1
	}
	procEnableWindow.Call(hwnd, value)
}

func utf16Ptr(value string) *uint16 {
	ptr, _ := syscall.UTF16PtrFromString(value)
	return ptr
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
