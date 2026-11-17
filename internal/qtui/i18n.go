package qtui

import (
	"embed"
	"io/fs"
	"log"
	"os"
	"strings"

	qt "github.com/mappu/miqt/qt6"

	"github.com/frontyard/whodat/internal/i18n"
)

//go:embed i18n/*.ts
var i18nFS embed.FS

// Languages lists the bundled translation locale codes ("de", "pt_BR"…)
// — the catalog files are i18n/whodat_<lang>.ts under internal/qtui.
func Languages() []string {
	var out []string
	entries, err := fs.ReadDir(i18nFS, "i18n")
	if err != nil {
		return out
	}
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, "whodat_") && strings.HasSuffix(n, ".ts") {
			out = append(out, strings.TrimSuffix(strings.TrimPrefix(n, "whodat_"), ".ts"))
		}
	}
	return out
}

// setupI18n picks the UI language (WHODAT_LANG env → "language" pref →
// system UI languages), loads its catalog, and installs a QTranslator so
// qsTr in QML and i18n.T in Go both serve from it. Call after
// NewQApplication, before the engine loads QML.
func (h *Host) setupI18n() {
	uiLangs := qt.QLocale_System().UiLanguages()
	override := os.Getenv("WHODAT_LANG")
	if override == "" {
		override = h.app.Cfg.Snapshot().Language
	}
	lang := i18n.DetectLang(override, uiLangs, Languages())
	if lang == "" {
		return // English source strings
	}
	data, err := i18nFS.ReadFile("i18n/whodat_" + lang + ".ts")
	if err != nil {
		log.Printf("i18n: read whodat_%s.ts: %v", lang, err)
		return
	}
	cat, err := i18n.ParseTS(data)
	if err != nil {
		log.Printf("i18n: %v", err)
		return
	}
	i18n.SetCatalog(cat, lang)
	qt.QLocale_SetDefault(qt.NewQLocale2(lang))

	// The QTranslator subclass serves QML qsTr lookups; its Translate
	// virtual goes straight to the same catalog i18n.T uses.
	t := qt.NewQTranslator()
	t.OnTranslate(func(_ func(string, string, string, int) string,
		context, sourceText, _ string, n int) string {
		if s, ok := cat.Lookup(context, sourceText, n); ok {
			return s
		}
		return ""
	})
	// installTranslator reports failure on an "empty" translator — with the
	// data living in our Go catalog, the base isEmpty() is always true.
	t.OnIsEmpty(func(_ func() bool) bool { return len(cat.Messages()) == 0 })
	if !qt.QCoreApplication_InstallTranslator(t) {
		log.Printf("i18n: installTranslator failed")
		return
	}
	h.translator = t // keep the Go wrapper alive — GC frees the binding
	log.Printf("i18n: language %s", lang)
}
