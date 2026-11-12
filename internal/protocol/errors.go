package protocol

import (
	"context"
	"errors"
	"strings"
)

var (
	ErrNotConnected   = errors.New("not connected")
	ErrConnTimeout    = errors.New("timed out — check your connection")
	ErrUnreachable    = errors.New("can't reach the server — check your connection")
	ErrSessionExpired = errors.New("session expired — link the device again")
)

// UserErr rewrites library/network jargon into a toastable message — raw
// wire errors ("websocket not connected", "context deadline exceeded",
// "dial tcp …: i/o timeout") tell the user nothing actionable. Anything it
// doesn't recognise is returned unchanged so real messages still get through.
func UserErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled):
		return errors.New("cancelled")
	case errors.Is(err, context.DeadlineExceeded):
		return ErrConnTimeout
	}
	switch m := err.Error(); {
	case strings.Contains(m, "the store doesn't contain a device JID"):
		return ErrSessionExpired
	case strings.Contains(m, "websocket not connected"):
		return ErrNotConnected
	case strings.Contains(m, "timed out"),
		strings.Contains(m, "deadline exceeded"),
		strings.Contains(m, "i/o timeout"):
		return ErrConnTimeout
	case strings.Contains(m, "no such host"),
		strings.Contains(m, "connection refused"),
		strings.Contains(m, "connection reset"),
		strings.Contains(m, "network is unreachable"),
		strings.Contains(m, "broken pipe"):
		return ErrUnreachable
	}
	return err
}
