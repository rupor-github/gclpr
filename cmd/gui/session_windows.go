//go:build windows

package main

import (
	"fmt"
	"log"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                    = windows.NewLazySystemDLL("user32.dll")
	pOpenInputDesktop         = user32.NewProc("OpenInputDesktop")
	pGetUserObjectInformation = user32.NewProc("GetUserObjectInformationW")
	pCloseDesktop             = user32.NewProc("CloseDesktop")
)

const (
	uoiName = 2
)

func currentSessionLocked() (bool, error) {
	hdesk, _, err := pOpenInputDesktop.Call(0, 0, windows.GENERIC_READ)
	if hdesk == 0 {
		if err == windows.ERROR_SUCCESS {
			err = windows.GetLastError()
		}
		return false, fmt.Errorf("unable to open input desktop: %w", err)
	}
	defer pCloseDesktop.Call(hdesk)

	var needed uint32
	ret, _, err := pGetUserObjectInformation.Call(hdesk, uintptr(uoiName), 0, 0, uintptr(unsafe.Pointer(&needed)))
	if ret == 0 && needed == 0 {
		if err == windows.ERROR_SUCCESS {
			err = windows.GetLastError()
		}
		return false, fmt.Errorf("unable to query input desktop name size: %w", err)
	}

	buf := make([]uint16, needed/2+1)
	ret, _, err = pGetUserObjectInformation.Call(
		hdesk,
		uintptr(uoiName),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)*2),
		uintptr(unsafe.Pointer(&needed)),
	)
	if ret == 0 {
		if err == windows.ERROR_SUCCESS {
			err = windows.GetLastError()
		}
		return false, fmt.Errorf("unable to query input desktop name: %w", err)
	}

	name := strings.TrimRight(windows.UTF16ToString(buf), "\x00")
	log.Printf("Input desktop is %q", name)
	switch strings.ToLower(name) {
	case "default":
		return false, nil
	case "winlogon":
		return true, nil
	default:
		return false, fmt.Errorf("unexpected input desktop %q", name)
	}
}
