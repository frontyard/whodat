// Package i18n provides user-facing string translation. It is Qt-free so
// backend packages (store, core, wa) can mark strings without importing
// the UI stack; qtui installs the active catalog at startup.
//
// Strings are marked in code with T/Tc/Tn and in QML with qsTr. The
// extraction tool (tools/i18nextract) rebuilds the Qt Linguist .ts
// catalogs under internal/qtui/i18n/; the app embeds them and serves
// translations through a QTranslator at runtime.
package i18n

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// Catalog is one language's parsed translation table:
// context → source text → translation forms (1 slot, or nplurals slots
// for numerus messages).
type Catalog struct {
	Lang string
	msgs map[string]map[string][]string
}

// Lookup resolves (context, source) — n >= 0 selects a plural form.
// Returns ("", false) when there is no usable translation.
func (c *Catalog) Lookup(ctx, src string, n int) (string, bool) {
	if c == nil {
		return "", false
	}
	forms, ok := c.msgs[ctx][src]
	if !ok {
		return "", false
	}
	i := 0
	if n >= 0 {
		i = PluralIndex(c.Lang, n)
		if i >= len(forms) {
			i = len(forms) - 1
		}
	}
	if i >= len(forms) || forms[i] == "" {
		return "", false
	}
	return forms[i], true
}

// Messages exposes the raw context → source → forms table — used by the
// extraction tool when merging an existing catalog.
func (c *Catalog) Messages() map[string]map[string][]string {
	if c == nil {
		return nil
	}
	return c.msgs
}

var (
	mu   sync.RWMutex
	cat  *Catalog
	lang string
)

// SetCatalog installs the active translation table; nil restores English
// passthrough. language is the catalog's locale code ("de", "pt_BR") —
// "" for the English source.
func SetCatalog(c *Catalog, language string) {
	mu.Lock()
	cat, lang = c, language
	mu.Unlock()
}

// Language reports the active locale code ("" = English source strings).
func Language() string {
	mu.RLock()
	defer mu.RUnlock()
	return lang
}

// lookup is the package-level query (English fallback returns "").
func lookup(ctx, src string, n int) (string, bool) {
	mu.RLock()
	c := cat
	mu.RUnlock()
	return c.Lookup(ctx, src, n)
}

// T translates a UI string in the default "Go" context. %1..%9 placeholders
// take args in order (fmt.Sprint formatting).
func T(src string, args ...any) string {
	out, _ := lookup("Go", src, -1)
	if out == "" {
		out = src
	}
	return subst(out, -1, args)
}

// Tc is T with an explicit context — for short/ambiguous strings whose
// meaning depends on where they appear.
func Tc(ctx, src string, args ...any) string {
	out, _ := lookup(ctx, src, -1)
	if out == "" {
		out = src
	}
	return subst(out, -1, args)
}

// Tn is the plural form of T: singular is the source key, plural is the
// English fallback for n != 1. %n substitutes the count; %1.. take args.
func Tn(singular, plural string, n int, args ...any) string {
	out, ok := lookup("Go", singular, n)
	if !ok {
		if n == 1 {
			out = singular
		} else {
			out = plural
		}
	}
	return subst(out, n, args)
}

// subst fills %1..%9 from args and %n from n (when >= 0). %% is not a Qt
// escape — a literal percent is written as-is.
func subst(s string, n int, args []any) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '%' || i+1 >= len(s) {
			b.WriteByte(c)
			continue
		}
		d := s[i+1]
		switch {
		case d == 'n' && n >= 0:
			b.WriteString(strconv.Itoa(n))
			i++
		case d >= '1' && d <= '9':
			k := int(d - '1')
			if k < len(args) {
				b.WriteString(fmt.Sprint(args[k]))
				i++
			} else {
				b.WriteByte(c) // missing arg — leave the placeholder
			}
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// DetectLang picks the catalog to load. override (config/env, "" = follow
// the OS) wins; then uiLanguages in order. Each candidate falls back
// specific→generic ("de_AT" → "de"). English locales match the built-in
// source strings and return "".
func DetectLang(override string, uiLangs, available []string) string {
	avail := make(map[string]string, len(available))
	for _, a := range available {
		avail[strings.ToLower(a)] = a
	}
	// match reports (catalog, done) — done=true means this candidate
	// settled the question (a hit, or an explicit English locale).
	match := func(l string) (string, bool) {
		c := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(l), "-", "_"))
		for c != "" {
			if v, ok := avail[c]; ok {
				return v, true
			}
			if c == "c" || c == "en" || strings.HasPrefix(c, "en_") {
				return "", true
			}
			i := strings.LastIndex(c, "_")
			if i < 0 {
				return "", false
			}
			c = c[:i]
		}
		return "", false
	}
	if override != "" && override != "system" {
		r, _ := match(override)
		return r
	}
	for _, l := range uiLangs {
		if r, done := match(l); done {
			return r
		}
	}
	return ""
}
