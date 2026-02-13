//go:build windows

package util

import (
	"log"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modUser32   = windows.NewLazySystemDLL("user32")
	pMessageBox = modUser32.NewProc("MessageBoxW")
)

// MsgType specifies how message box will look and behave.
type MsgType uint32

// Actual values.
const (
	MsgError       MsgType = 0x00000010 // MB_ICONHAND
	MsgExclamation MsgType = 0x00000030 // MB_ICONEXCLAMATION
	MsgInformation MsgType = 0x00000040 // MB_ICONASTERISK
)

// ShowOKMessage shows simple MB_OK message box.
func ShowOKMessage(t MsgType, title, text string) {

	log.Print(text)

	pText, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	pTitle, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	_, _, _ = pMessageBox.Call(0,
		uintptr(unsafe.Pointer(pText)),
		uintptr(unsafe.Pointer(pTitle)),
		uintptr(t))
}
