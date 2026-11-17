package i18n

import "testing"

func TestTPassthrough(t *testing.T) {
	SetCatalog(nil, "")
	if got := T("Hello %1", "world"); got != "Hello world" {
		t.Fatalf("T fallback: %q", got)
	}
	if got := Tn("%n unread", "%n unread", 3); got != "3 unread" {
		t.Fatalf("Tn fallback: %q", got)
	}
	if got := Tn("%n unread", "%n unread", 1); got != "1 unread" {
		t.Fatalf("Tn singular: %q", got)
	}
}

const deTS = `<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE TS>
<TS version="2.1" language="de">
<context>
    <name>Go</name>
    <message>
        <source>Quit</source>
        <translation>Beenden</translation>
    </message>
    <message>
        <source>Settings</source>
        <translation type="unfinished"></translation>
    </message>
    <message numerus="yes">
        <source>%n unread</source>
        <translation>
            <numerusform>%n ungelesen</numerusform>
            <numerusform>%n ungelesen</numerusform>
        </translation>
    </message>
</context>
<context>
    <name>ChatPage</name>
    <message>
        <source>Call</source>
        <translation>Anruf</translation>
    </message>
</context>
</TS>`

func TestParseTSAndLookup(t *testing.T) {
	c, err := ParseTS([]byte(deTS))
	if err != nil {
		t.Fatal(err)
	}
	if c.Lang != "de" {
		t.Fatalf("lang: %q", c.Lang)
	}
	if got, ok := c.Lookup("Go", "Quit", -1); !ok || got != "Beenden" {
		t.Fatalf("Quit: %q %v", got, ok)
	}
	// Unfinished must not be used.
	if got, ok := c.Lookup("Go", "Settings", -1); ok || got != "" {
		t.Fatalf("unfinished leaked: %q", got)
	}
	// Context isolation.
	if got, ok := c.Lookup("ChatPage", "Call", -1); !ok || got != "Anruf" {
		t.Fatalf("ctx lookup: %q %v", got, ok)
	}
	if _, ok := c.Lookup("Go", "Call", -1); ok {
		t.Fatal("context leak")
	}
	// Numerus: n=3 → de rule n!=1 → slot 1.
	if got, ok := c.Lookup("Go", "%n unread", 3); !ok || got != "%n ungelesen" {
		t.Fatalf("numerus: %q %v", got, ok)
	}
}

func TestTWithCatalog(t *testing.T) {
	c, err := ParseTS([]byte(deTS))
	if err != nil {
		t.Fatal(err)
	}
	SetCatalog(c, "de")
	defer SetCatalog(nil, "")
	if got := T("Quit"); got != "Beenden" {
		t.Fatalf("T: %q", got)
	}
	if got := T("Settings"); got != "Settings" {
		t.Fatalf("T fallback: %q", got)
	}
	if got := Tn("%n unread", "%n unread", 5); got != "5 ungelesen" {
		t.Fatalf("Tn: %q", got)
	}
	// Missing entry falls back to English plural rule.
	if got := Tn("%n file", "%n files", 2); got != "%n files" && got != "2 files" {
		t.Fatalf("Tn fallback: %q", got)
	}
}

func TestSubst(t *testing.T) {
	if got := subst("%1 — %2", -1, []any{"a", 42}); got != "a — 42" {
		t.Fatalf("subst: %q", got)
	}
	// Missing arg leaves the placeholder.
	if got := subst("%1 %2", -1, []any{"a"}); got != "a %2" {
		t.Fatalf("subst missing: %q", got)
	}
	if got := subst("%n unread", 7, nil); got != "7 unread" {
		t.Fatalf("subst n: %q", got)
	}
}

func TestDetectLang(t *testing.T) {
	avail := []string{"de", "fr", "pt_BR"}
	tests := []struct {
		name     string
		override string
		ui       []string
		want     string
	}{
		{"system en", "", []string{"en_US"}, ""},
		{"system de", "", []string{"de_DE", "en_US"}, "de"},
		{"system pt", "", []string{"pt_BR"}, "pt_BR"},
		{"specific falls back", "", []string{"de_AT"}, "de"},
		{"no match", "", []string{"ja"}, ""},
		{"override wins", "fr", []string{"de"}, "fr"},
		{"override en forces english", "en", []string{"de"}, ""},
		{"override unknown", "xx", []string{"de"}, ""},
		{"dash norm", "pt-br", nil, "pt_BR"},
	}
	for _, tt := range tests {
		if got := DetectLang(tt.override, tt.ui, avail); got != tt.want {
			t.Errorf("%s: got %q want %q", tt.name, got, tt.want)
		}
	}
}

func TestPluralRules(t *testing.T) {
	tests := []struct {
		lang string
		n    int
		want int
	}{
		{"en", 1, 0}, {"en", 0, 1}, {"en", 5, 1},
		{"de", 1, 0}, {"de", 2, 1},
		{"fr", 0, 0}, {"fr", 1, 0}, {"fr", 2, 1},
		{"ru", 1, 0}, {"ru", 2, 1}, {"ru", 5, 2}, {"ru", 11, 2}, {"ru", 21, 0}, {"ru", 111, 2},
		{"pl", 1, 0}, {"pl", 3, 1}, {"pl", 5, 2}, {"pl", 12, 2}, {"pl", 22, 1},
		{"cs", 1, 0}, {"cs", 3, 1}, {"cs", 5, 2},
		{"ja", 1, 0}, {"ja", 99, 0},
		{"ar", 0, 0}, {"ar", 1, 1}, {"ar", 2, 2}, {"ar", 5, 3}, {"ar", 11, 4}, {"ar", 100, 5},
	}
	for _, tt := range tests {
		if got := PluralIndex(tt.lang, tt.n); got != tt.want {
			t.Errorf("PluralIndex(%s,%d) = %d, want %d", tt.lang, tt.n, got, tt.want)
		}
	}
	if PluralCount("ru") != 3 || PluralCount("ar") != 6 || PluralCount("ja") != 1 {
		t.Fatal("PluralCount")
	}
}
