// Package guest is the host↔guest helper protocol (BRIEF-CI-P4 D3;
// specs/ci-execution-contract.md §4): versioned, bounded, per-attempt
// frames over one stream (vsock in the VM, a pipe in tests). The host
// side is Session, the guest side Server; both share the framing and
// the tar validation here. Nothing in this package opens a host path
// on the guest's say-so, and nothing trusts a length the other side
// announced beyond the bound.
package guest

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// ProtocolVersion is the helper protocol version in every frame and in
// the guest image manifest; the daemon refuses a guest of another.
const ProtocolVersion = 1

// MaxPayload bounds every frame (QUESTIONS §4, proposed 1 MiB).
const MaxPayload = 1 << 20

// Frame types. Host→guest are below 0x80; guest→host above.
const (
	TypeHello    = 0x01
	TypePutBegin = 0x02
	TypePutData  = 0x03
	TypePutEnd   = 0x04
	TypeExec     = 0x05
	TypeSignal   = 0x06
	TypeExport   = 0x07
	TypeShutdown = 0x08
	TypeVerify   = 0x09

	TypeReady      = 0x81
	TypePutOK      = 0x82
	TypeOutput     = 0x83
	TypeExit       = 0x84
	TypeExportData = 0x85
	TypeExportEnd  = 0x86
	TypeVerified   = 0x87
	TypeError      = 0xff
)

var (
	ErrFrameTooLarge = errors.New("guest protocol: frame exceeds the bound")
	ErrBadMagic      = errors.New("guest protocol: bad frame magic")
	ErrBadVersion    = errors.New("guest protocol: unsupported protocol version")
	ErrProtocol      = errors.New("guest protocol violation")
)

const headerLen = 8

// WriteFrame writes one frame; a payload over the bound is refused
// before a byte leaves.
func WriteFrame(w io.Writer, typ byte, payload []byte) error {
	if len(payload) > MaxPayload {
		return fmt.Errorf("%w: %d bytes", ErrFrameTooLarge, len(payload))
	}
	head := make([]byte, headerLen, headerLen+len(payload))
	head[0], head[1] = 'U', 'G'
	head[2] = ProtocolVersion
	head[3] = typ
	binary.BigEndian.PutUint32(head[4:8], uint32(len(payload)))
	_, err := w.Write(append(head, payload...))
	return err
}

// ReadFrame reads one frame; the announced length is checked against the
// bound before any allocation, and a short payload is an error, never a
// partial frame.
func ReadFrame(r io.Reader, maxPayload int) (byte, []byte, error) {
	head := make([]byte, headerLen)
	if _, err := io.ReadFull(r, head); err != nil {
		return 0, nil, err
	}
	if head[0] != 'U' || head[1] != 'G' {
		return 0, nil, ErrBadMagic
	}
	if head[2] != ProtocolVersion {
		return 0, nil, fmt.Errorf("%w: %d", ErrBadVersion, head[2])
	}
	n := binary.BigEndian.Uint32(head[4:8])
	if int(n) > maxPayload {
		return 0, nil, fmt.Errorf("%w: %d bytes announced", ErrFrameTooLarge, n)
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, fmt.Errorf("guest protocol: truncated frame: %w", err)
	}
	return head[3], payload, nil
}

// Control payloads (JSON).

type Hello struct {
	Attempt string `json:"attempt"`
	Nonce   string `json:"nonce"`
}

type Ready struct {
	Helper    int    `json:"helper"`
	Kernel    string `json:"kernel"`
	Docker    string `json:"docker"`
	MachineID string `json:"machine_id"`
	Hostname  string `json:"hostname"`
	// Bridge is the guest's address on the job network (the bridge
	// gateway): where act's cache and artifact servers bind so job
	// containers reach them without any route out of the guest
	Bridge string `json:"bridge,omitempty"`
}

type PutBegin struct {
	Dest       string `json:"dest"`
	MaxBytes   int64  `json:"max_bytes"`
	MaxEntries int    `json:"max_entries"`
}

type PutEnd struct {
	SHA256 string `json:"sha256"`
}

type PutOK struct {
	Entries int    `json:"entries"`
	Bytes   int64  `json:"bytes"`
	SHA256  string `json:"sha256"`
}

// Verify asks the guest to recompute the workspace digest of a path
// under its root and compare it with the host's; the answer is a
// loobean and the guest's own digest (for the log).
type Verify struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type Verified struct {
	OK     bool   `json:"ok"`
	SHA256 string `json:"sha256"`
}

type Exec struct {
	ID   uint32   `json:"id"`
	Argv []string `json:"argv"`
	Env  []string `json:"env"`
	Cwd  string   `json:"cwd"`
}

type Exit struct {
	ID   uint32 `json:"id"`
	Code int    `json:"code"`
}

type Signal struct {
	ID     uint32 `json:"id"`
	Signal string `json:"signal"`
}

type Export struct {
	Path     string `json:"path"`
	MaxBytes int64  `json:"max_bytes"`
}

type ExportEnd struct {
	Bytes     int64  `json:"bytes"`
	SHA256    string `json:"sha256"`
	Truncated bool   `json:"truncated"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
