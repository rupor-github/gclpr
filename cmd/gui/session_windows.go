//go:build windows

package main

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	wts32                       = windows.NewLazySystemDLL("Wtsapi32.dll")
	pWTSQuerySessionInformation = wts32.NewProc("WTSQuerySessionInformationW")
	pWTSFreeMemory              = wts32.NewProc("WTSFreeMemory")
)

const (
	wtsCurrentSession      = ^uint32(0)
	wtsInfoClassSessionEx  = 25
	wtsSessionStateLock    = 0
	wtsSessionStateOpen    = 1
	wtsSessionStateUnknown = -1
)

type wtsSessionInfoExData struct {
	Level        uint32
	SessionID    uint32
	SessionState uint32
	SessionFlags int32
}

func currentSessionLocked() (bool, error) {
	var buf uintptr
	var size uint32
	ret, _, err := pWTSQuerySessionInformation.Call(
		0,
		uintptr(wtsCurrentSession),
		uintptr(wtsInfoClassSessionEx),
		uintptr(unsafe.Pointer(&buf)),
		uintptr(unsafe.Pointer(&size)),
	)
	if ret == 0 {
		if err == windows.ERROR_SUCCESS {
			err = windows.GetLastError()
		}
		return false, fmt.Errorf("unable to query current session information: %w", err)
	}
	defer pWTSFreeMemory.Call(buf)

	if size < uint32(unsafe.Sizeof(wtsSessionInfoExData{})) {
		return false, fmt.Errorf("unexpected session info size %d", size)
	}
	info := (*wtsSessionInfoExData)(unsafe.Pointer(buf))
	if info.Level != 1 {
		return false, fmt.Errorf("unexpected session info level %d", info.Level)
	}

	switch info.SessionFlags {
	case wtsSessionStateLock:
		return true, nil
	case wtsSessionStateOpen:
		return false, nil
	case wtsSessionStateUnknown:
		return false, fmt.Errorf("current session lock state is unknown")
	default:
		return false, fmt.Errorf("unexpected session flags %d", info.SessionFlags)
	}
}
