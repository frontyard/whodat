package qtui

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/frontyard/whodat/internal/protocol"
	"github.com/frontyard/whodat/internal/store"
)

// registerAuthOps adds the multi-account auth commands. The generic
// start/submit/cancel flow is stage-driven — the QML never branches on
// protocol, only on the stage the service publishes via EvAuthStage.
func (h *Host) registerAuthOps() {
	h.ops["authStart"] = opAuthStart
	h.ops["authSubmit"] = opAuthSubmit
	h.ops["authCancel"] = opAuthCancel
	h.ops["logoutAccount"] = opLogoutAccount
	h.ops["listAccounts"] = opListAccounts
	h.ops["listBackends"] = opListBackends
}

// opAuthStart creates an account for a protocol and starts its auth flow.
// args: {proto} → {acct, stages}
func opAuthStart(h *Host, args json.RawMessage) (any, error) {
	var a struct {
		Proto string `json:"proto"`
	}
	if err := json.Unmarshal(args, &a); err != nil || a.Proto == "" {
		return nil, errors.New("proto required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	// An unpaired account of this protocol is a link-in-progress — resume
	// it rather than allocating a second slot the registry would shadow.
	var svc protocol.Service
	h.app.EachSvc(func(_ string, s protocol.Service) {
		if svc == nil && s.Protocol() == a.Proto && !s.Paired() {
			svc = s
		}
	})
	if svc == nil {
		var err error
		if svc, err = h.app.AddAccount(ctx, a.Proto); err != nil {
			return nil, err
		}
	}
	mt(func() { h.setState("authAcct", svc.Acct()) })
	// Connect kicks the auth flow for unpaired accounts (QR stream for WA,
	// phone prompt for TG); stages arrive as EvAuthStage events.
	go func() {
		if err := svc.Connect(context.Background()); err != nil {
			h.app.Bus.For(svc.Acct()).Publish(protocol.Event{Kind: protocol.EvAuthStage,
				AuthStage: protocol.StageFailed, AuthHint: err.Error()})
		}
	}()
	return map[string]any{"acct": svc.Acct(), "stages": svc.AuthStages()}, nil
}

// opAuthSubmit feeds user input for the current auth stage.
// args: {acct, stage, value} → {}
func opAuthSubmit(h *Host, args json.RawMessage) (any, error) {
	var a struct {
		Acct  string `json:"acct"`
		Stage string `json:"stage"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(args, &a); err != nil || a.Acct == "" {
		return nil, errors.New("acct required")
	}
	svc := h.app.Svc(a.Acct)
	if svc == nil {
		return nil, errors.New("no such account")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := svc.SubmitAuth(ctx, a.Stage, a.Value); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

// opAuthCancel abandons an unpaired account's auth — drops it from the
// registry before the wipe so the EvLoggedOut restart path can't resurrect
// it. args: {acct} → {}
func opAuthCancel(h *Host, args json.RawMessage) (any, error) {
	var a struct {
		Acct string `json:"acct"`
	}
	if err := json.Unmarshal(args, &a); err != nil || a.Acct == "" {
		return nil, errors.New("acct required")
	}
	svc := h.app.Svc(a.Acct)
	if svc == nil {
		return nil, errors.New("no such account")
	}
	if svc.Paired() {
		return nil, errors.New("account is paired — use logoutAccount")
	}
	h.app.RemoveAccount(context.Background(), a.Acct)
	svc.Disconnect()
	go svc.Logout(context.Background())
	mt(func() {
		h.setState("authAcct", "")
		h.setState("authStage", "")
		h.setState("authHint", "")
		h.setState("qrPath", "")
	})
	return map[string]any{}, nil
}

// opLogoutAccount unlinks one account and erases its local data. Remove
// precedes Logout so the EvLoggedOut handler sees the acct is gone and
// doesn't restart pairing on it. args: {acct} → {}
func opLogoutAccount(h *Host, args json.RawMessage) (any, error) {
	var a struct {
		Acct string `json:"acct"`
	}
	if err := json.Unmarshal(args, &a); err != nil || a.Acct == "" {
		return nil, errors.New("acct required")
	}
	svc := h.app.Svc(a.Acct)
	if svc == nil {
		return nil, errors.New("no such account")
	}
	h.app.RemoveAccount(context.Background(), a.Acct)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := svc.Logout(ctx); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

// opListAccounts returns accounts with live status. Only paired accounts
// can be registered (unlinked registrations are never persisted and are
// dropped at load). → {accounts: [{acct, proto, name, paired, ownId,
// connState, dirSearch, privacy}]}
func opListAccounts(h *Host, _ json.RawMessage) (any, error) {
	infos := h.app.Accounts()
	out := make([]map[string]any, 0, len(infos))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, i := range infos {
		var caps protocol.Caps
		if svc := h.app.Svc(i.Acct); svc != nil {
			caps = svc.Caps()
		}
		own := ""
		if i.OwnID != "" {
			// The list wants a readable handle ("+1234…"), not the
			// namespaced storage id ("wa:1234@…", "wa2:5678@…").
			own = h.app.NumberOf(ctx, store.PeerID(i.OwnID))
		}
		out = append(out, map[string]any{
			"acct":      i.Acct,
			"proto":     i.Proto,
			"name":      i.Name,
			"paired":    i.Paired,
			"ownId":     own,
			"connState": h.acctState(i.Acct),
			"dirSearch": caps.DirectorySearch,
			"privacy":   caps.Privacy,
		})
	}
	return map[string]any{"accounts": out}, nil
}

// opListBackends returns the registered protocols for the add-account
// picker. canAdd flags a non-multi backend as taken only once an account
// is actually linked — a registered-but-unpaired slot resumes instead of
// blocking. → {backends: [{proto, name, canAdd}]}
func opListBackends(h *Host, _ json.RawMessage) (any, error) {
	linked := map[string]int{}
	h.app.EachSvc(func(_ string, s protocol.Service) {
		if s.Paired() {
			linked[s.Protocol()]++
		}
	})
	var out []map[string]any
	for _, b := range protocol.Backends() {
		out = append(out, map[string]any{
			"proto":  b.Proto,
			"name":   b.Name,
			"canAdd": b.Multi || linked[b.Proto] == 0,
		})
	}
	return map[string]any{"backends": out}, nil
}
