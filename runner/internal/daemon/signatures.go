package daemon

import (
	"errors"
	"fmt"
	"time"

	"urgit/runner/internal/ship"
	"urgit/runner/internal/signing"
)

func grantCurrent(g ship.Grant, now int64) bool { return g.Expiry > now }

func assignmentMessage(a *ship.Assignment) ([]byte, error) {
	// The captured object is decoded in the same UnmarshalJSON invocation
	// as the typed assignment, so execution and verification see the same
	// values. The four envelope fields are bound by Message separately.
	body := a.SigningBody()
	if body == nil {
		return nil, errors.New("assignment has no signed wire body")
	}
	op, err := signing.CanonicalJSON(body)
	if err != nil {
		return nil, err
	}
	return signing.Message(a.Recipient, a.Attempt, signing.Cell(signing.Cord("assignment"), op), a.Expiry, a.Nonce)
}
func grantMessage(a *ship.Assignment, g ship.Grant) ([]byte, error) {
	return signing.Message(a.Recipient, a.Attempt, signing.Tuple(signing.Cord("grant"), signing.Octets(g.Name), signing.Octets(g.Value)), g.Expiry, g.Nonce)
}
func (d *Daemon) verifyAssignment(a *ship.Assignment, now int64) error {
	if a.Recipient != d.daemonID {
		return errors.New("assignment recipient is not this daemon")
	}
	if a.Expiry <= now {
		return errors.New("assignment expired")
	}
	pub, err := signing.PublicKey(d.cfg.CIPub)
	if err != nil {
		return fmt.Errorf("CI public key: %w", err)
	}
	msg, err := assignmentMessage(a)
	if err != nil {
		return err
	}
	if err = signing.Verify(pub, a.Sig, msg); err != nil {
		return err
	}
	if (a.Trust != "trusted" || a.Kind != "job") && len(a.Grants) > 0 {
		return errors.New("untrusted or non-job attempt received credential grants")
	}
	for _, g := range a.Grants {
		if !grantCurrent(g, now) {
			return errors.New("credential grant expired")
		}
		msg, err = grantMessage(a, g)
		if err != nil {
			return err
		}
		if err = signing.Verify(pub, g.Sig, msg); err != nil {
			return fmt.Errorf("credential grant: %w", err)
		}
	}
	return nil
}

// Refusal happens before any work directory, sandbox or checkout. The
// sender may redeliver a legitimate assignment later; a bad signature
// is not an authenticated instruction to close the ship's attempt.
func (d *Daemon) signatureRefusal(a *ship.Assignment) bool {
	if err := d.verifyAssignment(a, time.Now().Unix()); err != nil {
		d.log.Printf("assignment refused: %s", err)
		return true
	}
	return false
}
