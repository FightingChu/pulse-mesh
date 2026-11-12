//go:build windows

package journal

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32Lock = syscall.NewLazyDLL("kernel32.dll")
	lockFileEx   = kernel32Lock.NewProc("LockFileEx")
	unlockFileEx = kernel32Lock.NewProc("UnlockFileEx")
)

type overlapped struct {
	Internal     uintptr
	InternalHigh uintptr
	Offset       uint32
	OffsetHigh   uint32
	Event        syscall.Handle
}

const lockfileExclusiveLock = 0x00000002
const lockfileFailImmediately = 0x00000001

func lockFile(file *os.File) error {
	var state overlapped
	result, _, callErr := lockFileEx.Call(
		uintptr(syscall.Handle(file.Fd())),
		lockfileExclusiveLock|lockfileFailImmediately,
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&state)),
	)
	if result == 0 {
		return callErr
	}
	return nil
}

func unlockFile(file *os.File) error {
	var state overlapped
	result, _, callErr := unlockFileEx.Call(
		uintptr(syscall.Handle(file.Fd())),
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&state)),
	)
	if result == 0 {
		return callErr
	}
	return nil
}
