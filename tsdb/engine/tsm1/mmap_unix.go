// +build !windows,!plan9,!solaris

package tsm1

import (
	"os"
	"syscall"
	"unsafe"
)

func mmap(f *os.File, offset int64, length int) ([]byte, error) {
	// anonymous mapping
	if f == nil {
		return syscall.Mmap(-1, 0, length, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	}

	return syscall.Mmap(int(f.Fd()), 0, length, syscall.PROT_READ, syscall.MAP_SHARED)
}

func munmap(b []byte) (err error) {
	return syscall.Munmap(b)
}

func madvise(b []byte, advice int) error {
	// For some reason madvise is not in the stdlib for darwin
	_, _, errno := syscall.Syscall(syscall.SYS_MADVISE, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), uintptr(advice))
	if errno != 0 {
		return errno
	}
	return nil
}
