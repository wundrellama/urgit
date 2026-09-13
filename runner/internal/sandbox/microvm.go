package sandbox

// NewMicrovm is the P2 backend's constructor: the config keys exist and
// parse, the backend does not. No silent fallback to anything else.
func NewMicrovm() (Sandbox, error) {
	return nil, ErrMicrovmUnavailable
}
