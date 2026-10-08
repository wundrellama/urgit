package launcher

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

// ConnectVsock opens Firecracker's host-side vsock unix socket and asks
// it to connect to a guest port (the documented `CONNECT <port>\n` /
// `OK <n>\n` handshake), returning the connected descriptor as a file so
// it can be passed to the daemon over SCM_RIGHTS or used directly.
func ConnectVsock(udsPath string, port uint32, timeout time.Duration) (*os.File, error) {
	c, err := net.DialTimeout("unix", udsPath, timeout)
	if err != nil {
		return nil, err
	}
	uc := c.(*net.UnixConn)
	_ = uc.SetDeadline(time.Now().Add(timeout))
	if _, err := fmt.Fprintf(uc, "CONNECT %d\n", port); err != nil {
		uc.Close()
		return nil, err
	}
	line, err := bufio.NewReader(uc).ReadString('\n')
	if err != nil {
		uc.Close()
		return nil, fmt.Errorf("vsock handshake: %w", err)
	}
	if !strings.HasPrefix(line, "OK ") {
		uc.Close()
		return nil, fmt.Errorf("vsock handshake answered %q", strings.TrimSpace(line))
	}
	_ = uc.SetDeadline(time.Time{})
	f, err := uc.File()
	uc.Close()
	if err != nil {
		return nil, err
	}
	return f, nil
}
