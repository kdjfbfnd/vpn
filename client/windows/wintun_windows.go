package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

type wintunAdapter uintptr
type wintunSession uintptr

type wintunDLL struct {
	dll                  *syscall.LazyDLL
	createAdapter        *syscall.LazyProc
	closeAdapter         *syscall.LazyProc
	startSession         *syscall.LazyProc
	endSession           *syscall.LazyProc
	getReadWaitEvent     *syscall.LazyProc
	receivePacket        *syscall.LazyProc
	releaseReceivePacket *syscall.LazyProc
	allocateSendPacket   *syscall.LazyProc
	sendPacket           *syscall.LazyProc
}

func loadWintun() (*wintunDLL, error) {
	dll := syscall.NewLazyDLL("wintun.dll")
	api := &wintunDLL{
		dll:                  dll,
		createAdapter:        dll.NewProc("WintunCreateAdapter"),
		closeAdapter:         dll.NewProc("WintunCloseAdapter"),
		startSession:         dll.NewProc("WintunStartSession"),
		endSession:           dll.NewProc("WintunEndSession"),
		getReadWaitEvent:     dll.NewProc("WintunGetReadWaitEvent"),
		receivePacket:        dll.NewProc("WintunReceivePacket"),
		releaseReceivePacket: dll.NewProc("WintunReleaseReceivePacket"),
		allocateSendPacket:   dll.NewProc("WintunAllocateSendPacket"),
		sendPacket:           dll.NewProc("WintunSendPacket"),
	}
	if err := dll.Load(); err != nil {
		return nil, fmt.Errorf("load wintun.dll: %w", err)
	}
	return api, nil
}

func (w *wintunDLL) create(name string) (wintunAdapter, error) {
	pool, err := syscall.UTF16PtrFromString("SoloVPN")
	if err != nil {
		return 0, err
	}
	adapterName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	ret, _, callErr := w.createAdapter.Call(
		uintptr(unsafe.Pointer(pool)),
		uintptr(unsafe.Pointer(adapterName)),
		0,
	)
	if ret == 0 {
		return 0, callError("WintunCreateAdapter", callErr)
	}
	return wintunAdapter(ret), nil
}

func (w *wintunDLL) close(adapter wintunAdapter) {
	if adapter != 0 {
		w.closeAdapter.Call(uintptr(adapter))
	}
}

func (w *wintunDLL) start(adapter wintunAdapter, capacity uint32) (wintunSession, error) {
	ret, _, callErr := w.startSession.Call(uintptr(adapter), uintptr(capacity))
	if ret == 0 {
		return 0, callError("WintunStartSession", callErr)
	}
	return wintunSession(ret), nil
}

func (w *wintunDLL) end(session wintunSession) {
	if session != 0 {
		w.endSession.Call(uintptr(session))
	}
}

func (w *wintunDLL) readWaitEvent(session wintunSession) syscall.Handle {
	ret, _, _ := w.getReadWaitEvent.Call(uintptr(session))
	return syscall.Handle(ret)
}

func (w *wintunDLL) receive(session wintunSession) ([]byte, uintptr, error) {
	var size uint32
	ret, _, callErr := w.receivePacket.Call(uintptr(session), uintptr(unsafe.Pointer(&size)))
	if ret == 0 {
		return nil, 0, callErr
	}
	packet := unsafe.Slice((*byte)(unsafe.Pointer(ret)), int(size))
	return packet, ret, nil
}

func (w *wintunDLL) release(session wintunSession, packet uintptr) {
	w.releaseReceivePacket.Call(uintptr(session), packet)
}

func (w *wintunDLL) send(session wintunSession, packet []byte) error {
	ptr, _, callErr := w.allocateSendPacket.Call(uintptr(session), uintptr(len(packet)))
	if ptr == 0 {
		return callError("WintunAllocateSendPacket", callErr)
	}
	dst := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), len(packet))
	copy(dst, packet)
	ret, _, callErr := w.sendPacket.Call(uintptr(session), ptr)
	if ret == 0 {
		return callError("WintunSendPacket", callErr)
	}
	return nil
}

func callError(name string, err error) error {
	if errno, ok := err.(syscall.Errno); ok && errno == 0 {
		return fmt.Errorf("%s failed", name)
	}
	return fmt.Errorf("%s failed: %w", name, err)
}
