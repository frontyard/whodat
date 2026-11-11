package wa

import (
	"context"
	"errors"

	"github.com/frontyard/whodat/internal/protocol"
)

// AuthStages lists the login steps this service drives: the QR stream is
// the default; "phone" takes a number and yields an 8-digit pairing code.
func (s *Service) AuthStages() []string {
	return []string{string(protocol.StageQR), string(protocol.StagePhone)}
}

// StartAuth kicks the QR flow for an unpaired account (no-op when already
// running or paired — Connect starts it automatically too).
func (s *Service) StartAuth(ctx context.Context) error {
	if s.Paired() {
		return errors.New("already paired")
	}
	return s.startQR(ctx)
}

// SubmitAuth feeds an auth value: stage "phone" requests a pairing code,
// which arrives back as an EvAuthStage(paircode) event.
func (s *Service) SubmitAuth(ctx context.Context, stage, value string) error {
	switch protocol.AuthStage(stage) {
	case protocol.StagePhone:
		code, err := s.PairWithPhone(ctx, value)
		if err != nil {
			return err
		}
		s.sink.Publish(Event{Kind: EvAuthStage, AuthStage: protocol.StagePairCode, AuthHint: code})
		return nil
	}
	return errors.New("unsupported auth stage " + stage)
}
