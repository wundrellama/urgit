package guest

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// AF_VSOCK plumbing without a third-party module: the sockaddr_vm layout
// is the kernel ABI (include/uapi/linux/vm_sockets.h).
const (
	afVsock        = 40
	vmaddrCIDAny   = 0xffffffff
	vmaddrCIDHost  = 2
	HelperPort     = 5000
	sockaddrVMSize = 16
)

type sockaddrVM struct {
	family    uint16
	reserved1 uint16
	port      uint32
	cid       uint32
	zero      [4]byte
}

// ListenVsock binds a vsock listener on the given port for any CID and
// returns the listening fd.
func ListenVsock(port uint32) (int, error) {
	fd, err := syscall.Socket(afVsock, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("vsock socket: %w", err)
	}
	sa := sockaddrVM{family: afVsock, port: port, cid: vmaddrCIDAny}
	if _, _, e := syscall.Syscall(syscall.SYS_BIND, uintptr(fd), uintptr(unsafe.Pointer(&sa)), sockaddrVMSize); e != 0 {
		syscall.Close(fd)
		return -1, fmt.Errorf("vsock bind: %w", e)
	}
	if err := syscall.Listen(fd, 1); err != nil {
		syscall.Close(fd)
		return -1, fmt.Errorf("vsock listen: %w", err)
	}
	return fd, nil
}

// AcceptVsock accepts one connection and wraps it as a file.
func AcceptVsock(listenFD int) (*os.File, error) {
	var sa sockaddrVM
	size := uint32(sockaddrVMSize)
	nfd, _, e := syscall.Syscall(syscall.SYS_ACCEPT, uintptr(listenFD), uintptr(unsafe.Pointer(&sa)), uintptr(unsafe.Pointer(&size)))
	if e != 0 {
		return nil, fmt.Errorf("vsock accept: %w", e)
	}
	syscall.CloseOnExec(int(nfd))
	return os.NewFile(nfd, "vsock"), nil
}

// ErrNoVsock names the failure when the guest has no vsock device.
var ErrNoVsock = errors.New("no vsock device")
