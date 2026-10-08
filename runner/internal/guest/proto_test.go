package guest

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

// The frame layout is the contract (specs/ci-execution-contract.md §4):
// "UG", version 1, a type byte, a big-endian u32 length, the payload.

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, TypeHello, []byte(`{"attempt":"0v1"}`)); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	if string(raw[:2]) != "UG" || raw[2] != ProtocolVersion || raw[3] != TypeHello {
		t.Fatalf("header %x", raw[:4])
	}
	if binary.BigEndian.Uint32(raw[4:8]) != uint32(len(`{"attempt":"0v1"}`)) {
		t.Fatalf("length %x", raw[4:8])
	}
	typ, payload, err := ReadFrame(&buf, MaxPayload)
	if err != nil || typ != TypeHello || string(payload) != `{"attempt":"0v1"}` {
		t.Fatalf("read: %v %d %q", err, typ, payload)
	}
}

func TestFrameRefusesOversize(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{'U', 'G', ProtocolVersion, TypeOutput})
	binary.Write(&buf, binary.BigEndian, uint32(MaxPayload+1))
	_, _, err := ReadFrame(&buf, MaxPayload)
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("want ErrFrameTooLarge, got %v", err)
	}
	// the writer refuses too: a payload over the bound never leaves
	if err := WriteFrame(io.Discard, TypeOutput, make([]byte, MaxPayload+1)); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("write: want ErrFrameTooLarge, got %v", err)
	}
}

func TestFrameRefusesBadMagicAndVersion(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{'X', 'X', ProtocolVersion, TypeHello, 0, 0, 0, 0})
	if _, _, err := ReadFrame(&buf, MaxPayload); !errors.Is(err, ErrBadMagic) {
		t.Fatalf("magic: %v", err)
	}
	buf.Reset()
	buf.Write([]byte{'U', 'G', 9, TypeHello, 0, 0, 0, 0})
	if _, _, err := ReadFrame(&buf, MaxPayload); !errors.Is(err, ErrBadVersion) {
		t.Fatalf("version: %v", err)
	}
}

func TestFrameTruncatedIsAnError(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{'U', 'G', ProtocolVersion, TypeOutput})
	binary.Write(&buf, binary.BigEndian, uint32(10))
	buf.Write([]byte("short"))
	if _, _, err := ReadFrame(&buf, MaxPayload); err == nil {
		t.Fatal("a truncated frame must not read as a frame")
	}
}
