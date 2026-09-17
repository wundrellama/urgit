// Package state persists what enrollment returned: the daemon id and the
// bearer. The enrollment token is never written here (D7 b).
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type State struct {
	DaemonID string `json:"daemon_id"`
	Bearer   string `json:"bearer"`
	ShipURL  string `json:"ship_url"`
	// the ship's CI public key as handed over at enrollment (D5); every
	// assignment and grant must verify against it
	CIPublicKey string `json:"ci_public_key,omitempty"`
}

// Load returns nil, nil when the file does not exist.
func Load(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("state file %s: %w", path, err)
	}
	if s.DaemonID == "" || s.Bearer == "" {
		return nil, fmt.Errorf("state file %s: daemon_id and bearer are required", path)
	}
	return &s, nil
}

// Save writes the file with mode 0600, creating the directory, replacing
// atomically so a crash never leaves a half-written credential.
func Save(path string, s *State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
