package sig

import (
	"errors"
	"fmt"
	"math/big"
)

// RecoveryVersion is the recovery command message's version (legacy-
// recovery UI ruling 01; runner/launcher/INTEGRATION.md §11.12). A verifier
// of another version fails closed.
const RecoveryVersion = 1

// RecoveryTag leads every recovery command message: no signature over one
// verifies as an assignment's or a grant's (which lead with their version,
// 2), and none of theirs as a recovery command's.
const RecoveryTag = "recovery"

// RecoveryMessage is what the ship's CI key signs for one recovery command
// (desk/lib/ci-recovery.hoon): the recipient daemon, the command, its one
// operation, the expiry as unix seconds, a nonce, and the exact retention
// it is bound to — its selection, the revision the operator inspected, and
// the digest of the evidence shown.
type RecoveryMessage struct {
	Recipient string // @uv text
	Command   string // @uv text
	Operation string
	Expiry    int64
	Nonce     string // @uv text
	Selection string
	Revision  uint64
	Evidence  string
}

// Noun is ['recovery' 1 recipient command operation expiry nonce selection
// revision evidence], the ids and the nonce as the atoms their @uv text
// denotes.
func (m RecoveryMessage) Noun() (*Noun, error) {
	recipient, err := ParseUV(m.Recipient)
	if err != nil {
		return nil, fmt.Errorf("recipient: %w", err)
	}
	command, err := ParseUV(m.Command)
	if err != nil {
		return nil, fmt.Errorf("command: %w", err)
	}
	nonce, err := ParseUV(m.Nonce)
	if err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	if m.Expiry < 0 {
		return nil, errors.New("expiry before the epoch")
	}
	return Tuple(Cord(RecoveryTag), Atom(big.NewInt(RecoveryVersion)), Atom(recipient), Atom(command), Cord(m.Operation),
		Atom(big.NewInt(m.Expiry)), Atom(nonce), Cord(m.Selection), Atom(new(big.Int).SetUint64(m.Revision)), Cord(m.Evidence)), nil
}

// Bytes is what is signed: the little-endian bytes of the jam.
func (m RecoveryMessage) Bytes() ([]byte, error) {
	n, err := m.Noun()
	if err != nil {
		return nil, err
	}
	return LittleEndian(Jam(n)), nil
}

// VerifyRecovery checks sigHex over the recovery command message with the
// pinned public key.
func VerifyRecovery(pubHex string, m RecoveryMessage, sigHex string) error {
	n, err := m.Noun()
	if err != nil {
		return err
	}
	return verifyNoun(pubHex, n, sigHex)
}
