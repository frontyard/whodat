package protocol

import (
	"context"
	"errors"
	"strings"

	"github.com/frontyard/whodat/internal/i18n"
)

// The Err* values are message identity only — UserErr re-creates them with
// the message translated at call time, so language changes apply per toast.
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
		return errors.New(i18n.T("cancelled"))
	case errors.Is(err, context.DeadlineExceeded):
		return errors.New(i18n.T("timed out — check your connection"))
	}
	switch m := err.Error(); {
	case strings.Contains(m, "the store doesn't contain a device JID"):
		return errors.New(i18n.T("session expired — link the device again"))
	case strings.Contains(m, "websocket not connected"):
		return errors.New(i18n.T("not connected"))
	case strings.Contains(m, "timed out"),
		strings.Contains(m, "deadline exceeded"),
		strings.Contains(m, "i/o timeout"):
		return errors.New(i18n.T("timed out — check your connection"))
	case strings.Contains(m, "no such host"),
		strings.Contains(m, "connection refused"),
		strings.Contains(m, "connection reset"),
		strings.Contains(m, "network is unreachable"),
		strings.Contains(m, "broken pipe"):
		return errors.New(i18n.T("can't reach the server — check your connection"))
	}
	return err
}
