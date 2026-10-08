package launcher

// Exact identity and verified custody (runner/launcher/INTEGRATION.md §11):
// an incarnation is named by its token, never by a tuple a restart or a
// clock can repeat; a process is verified running, verified gone, or
// unknown, and unknown is never proof.

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// newIncarnation is a reservation's incarnation token: 128 bits from the
// kernel's random generator, as 32 lowercase hex characters (§11.1). An
// error refuses the admission: nothing is charged or written.
func newIncarnation() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("no incarnation token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// validIncarnation says whether s is an incarnation token: 32 lowercase
// hex characters.
func validIncarnation(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range s {
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f') {
			return false
		}
	}
	return true
}

// Ref names one incarnation of a reservation exactly (§11.1): its record's
// id and its incarnation token. A record written before tokens existed has
// none; it is named by its cid and creation time instead, and only such a
// record is named by a reference without a token.
type Ref struct {
	ID          string
	Incarnation string
	CID         uint32
	Created     int64
}

// Ref is the exact reference to r.
func (r Record) Ref() Ref {
	return Ref{ID: r.ID, Incarnation: r.Incarnation, CID: r.CID, Created: r.Created}
}

// Ref is the exact reference to the reservation reply names.
func (reply ReserveReply) Ref() Ref {
	return Ref{ID: reply.ID, Incarnation: reply.Incarnation, CID: reply.CID, Created: reply.Created}
}

// Named says whether ref names any incarnation at all: a token, or — for a
// record written before tokens — its cid and creation time.
func (ref Ref) Named() bool {
	return ref.ID != "" && (ref.Incarnation != "" || ref.CID != 0 || ref.Created != 0)
}

// Names says whether ref names r: with a token, the token decides (and the
// cid and creation time, where ref gives them, must agree); without one,
// only a record without one, by its cid and creation time. A token-less
// reference never names a tokened record, nor the reverse.
func (ref Ref) Names(r Record) bool {
	switch {
	case ref.ID != r.ID:
		return false
	case ref.Incarnation != "" || r.Incarnation != "":
		return ref.Incarnation == r.Incarnation && (ref.CID == 0 || ref.CID == r.CID) && (ref.Created == 0 || ref.Created == r.Created)
	}
	return ref.CID == r.CID && ref.Created == r.Created
}

// String is ref as the operator reads it.
func (ref Ref) String() string {
	if ref.Incarnation != "" {
		return fmt.Sprintf("%s (incarnation %s)", ref.ID, ref.Incarnation)
	}
	return fmt.Sprintf("%s (cid %d, created %d; no incarnation token: written before tokens)", ref.ID, ref.CID, ref.Created)
}

// Liveness is what a host can say about a VMM process of a record (§11.2):
// verified running as the id's VMM, verified gone, or not verifiable.
type Liveness int

const (
	// Gone: no such process, or a process verified to be another's — a
	// foreign process that took the pid, a zombie, a kernel thread.
	Gone Liveness = iota
	// Running: verified as the id's VMM (its command line carries exactly
	// `--id <id>`).
	Running
	// Unknown: not verifiable — never proof that the VMM is gone, never
	// grounds to signal the process.
	Unknown
)

func (l Liveness) String() string {
	switch l {
	case Gone:
		return "gone"
	case Running:
		return "running"
	}
	return "unknown"
}
