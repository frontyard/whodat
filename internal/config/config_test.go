package config

import (
	"path/filepath"
	"testing"

	"github.com/frontyard/whodat/internal/store"
)

// Media and avatar dirs are per-account so an account wipe can RemoveAll
// its own dir without ever touching a peer account's files.
func TestAccountDirs(t *testing.T) {
	if MediaAcctDir("wa") != filepath.Join(MediaDir(), "wa") {
		t.Fatalf("media dir: %s", MediaAcctDir("wa"))
	}
	if AvatarAcctDir("wa2") != filepath.Join(AvatarDir(), "wa2") {
		t.Fatalf("avatar dir: %s", AvatarAcctDir("wa2"))
	}
	// Same peer, two accounts → different directories.
	wa := MediaIDDir(store.PeerID("wa:x@s.whatsapp.net"))
	wa2 := MediaIDDir(store.PeerID("wa2:x@s.whatsapp.net"))
	if wa == wa2 {
		t.Fatalf("shared media dir: %s", wa)
	}
	if AvatarIDDir(store.PeerID("wa:x")) != AvatarIDDir(store.PeerID("wa:y")) {
		t.Fatal("same account should share one avatar dir")
	}
	if AvatarIDDir(store.PeerID("wa:x")) == AvatarIDDir(store.PeerID("wa2:x")) {
		t.Fatal("different accounts must not share an avatar dir")
	}
	// AvatarPath lands inside the account's dir.
	p := AvatarPath(store.PeerID("wa2:x@s.whatsapp.net"))
	if filepath.Dir(p) != AvatarAcctDir("wa2") {
		t.Fatalf("avatar path outside acct dir: %s", p)
	}
}
