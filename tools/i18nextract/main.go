// i18nextract is whodat's mini-lupdate: it scans the QML files for
// qsTr/qsTranslate calls and the Go sources for i18n.T/Tc/Tn calls, then
// regenerates the Qt Linguist catalog template internal/qtui/i18n/whodat.ts
// and merges every existing whodat_<lang>.ts (keeping translations whose
// source strings still exist, adding new sources as unfinished).
//
// Run via `make i18n` — or `go run ./tools/i18nextract`.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/frontyard/whodat/internal/i18n"
)

const (
	catDir  = "internal/qtui/i18n"
	qmlDir  = "internal/qtui/qml"
	tplFile = "whodat.ts" // template — English source, all unfinished
)

// msg is one translatable unit keyed by (context, source).
type msg struct {
	source  string
	comment string // qsTr disambiguation — kept for translators
	plural  string // English plural source — non-empty marks numerus
}

// catalog is ctx → insertion-ordered unique messages.
type catalog struct {
	order []string
	ctxs  map[string][]msg
	seen  map[string]map[string]int // ctx → source → index in ctxs
}

func newCatalog() *catalog {
	return &catalog{ctxs: map[string][]msg{}, seen: map[string]map[string]int{}}
}

func (c *catalog) add(ctx, source, comment, plural string) {
	if source == "" {
		return
	}
	sm := c.seen[ctx]
	if sm == nil {
		sm = map[string]int{}
		c.seen[ctx] = sm
		c.order = append(c.order, ctx)
	}
	if i, ok := sm[source]; ok {
		m := &c.ctxs[ctx][i]
		if plural != "" {
			m.plural = plural // a Tn/qsTr(,,n) call wins over T
		}
		return
	}
	sm[source] = len(c.ctxs[ctx])
	c.ctxs[ctx] = append(c.ctxs[ctx], msg{source: source, comment: comment, plural: plural})
}

func main() {
	log.SetFlags(0)
	root, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	cat := newCatalog()
	if err := scanQML(filepath.Join(root, qmlDir), cat); err != nil {
		log.Fatal(err)
	}
	if err := scanGo(root, cat); err != nil {
		log.Fatal(err)
	}
	n := 0
	for _, ms := range cat.ctxs {
		n += len(ms)
	}
	log.Printf("i18n: extracted %d messages in %d contexts", n, len(cat.ctxs))

	if err := writeTS(filepath.Join(root, catDir, tplFile), "", cat, nil); err != nil {
		log.Fatal(err)
	}
	// Merge into each whodat_<lang>.ts, preserving existing translations.
	entries, _ := filepath.Glob(filepath.Join(root, catDir, "whodat_*.ts"))
	for _, f := range entries {
		lang := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "whodat_"), ".ts")
		old, err := readTranslations(f)
		if err != nil {
			log.Printf("i18n: skip %s: %v", f, err)
			continue
		}
		if err := writeTS(f, lang, cat, old); err != nil {
			log.Fatal(err)
		}
	}
}

// ------------------------------------------------------------- Go scan ----

func scanGo(root string, cat *catalog) error {
	fset := token.NewFileSet()
	var paths []string
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") ||
				strings.HasSuffix(p, "_test.go") {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			if strings.HasPrefix(rel, "tools"+string(filepath.Separator)) {
				return nil // the extractor's own strings are English-only
			}
			paths = append(paths, p)
			return nil
		})
		if err != nil {
			return err
		}
	}
	sort.Strings(paths)
	for _, p := range paths {
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", p, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			x, ok := sel.X.(*ast.Ident)
			if !ok || x.Name != "i18n" {
				return true
			}
			switch sel.Sel.Name {
			case "T":
				if s := litArg(call, 0); s != "" {
					cat.add("Go", s, "", "")
				}
			case "Tc":
				ctx, s := litArg(call, 0), litArg(call, 1)
				if ctx != "" && s != "" {
					cat.add(ctx, s, "", "")
				}
			case "Tn":
				sing, plur := litArg(call, 0), litArg(call, 1)
				if sing != "" && plur != "" {
					cat.add("Go", sing, "", plur)
				}
			}
			return true
		})
	}
	return nil
}

// litArg returns the string literal value of call.Args[i], "" when the arg
// isn't a plain literal (constants need no translation anyway — mark with
// literals).
func litArg(call *ast.CallExpr, i int) string {
	if i >= len(call.Args) {
		return ""
	}
	bl, ok := call.Args[i].(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return ""
	}
	s, err := strconv.Unquote(bl.Value)
	if err != nil {
		return ""
	}
	return s
}

// ------------------------------------------------------------ QML scan ----

var reQSTr = regexp.MustCompile(`\bqsTr(?:anslate)?\s*\(`)

func scanQML(dir string, cat *catalog) error {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var files []string
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".qml") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	for _, name := range files {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		ctx := strings.TrimSuffix(name, ".qml")
		src := string(b)
		for _, loc := range reQSTr.FindAllStringIndex(src, -1) {
			args, _ := parseCallArgs(src, loc[1])
			isTranslate := strings.HasPrefix(src[loc[0]:loc[1]], "qsTranslate")
			if isTranslate {
				// qsTranslate("Ctx", "src", [comment], [n])
				if len(args) >= 2 {
					if c, ok1 := jsString(args[0]); ok1 {
						if s, ok2 := jsString(args[1]); ok2 {
							cat.add(c, s, "", ternary(len(args) >= 4, s, ""))
						}
					}
				}
				continue
			}
			// qsTr("src", [comment], [n]) — a numerus call shares one
			// source for all plural slots (Qt substitutes %n itself).
			if len(args) >= 1 {
				if s, ok := jsString(args[0]); ok {
					cat.add(ctx, s, "", ternary(len(args) >= 3, s, ""))
				}
			}
		}
	}
	return nil
}

func ternary[T any](c bool, a, b T) T {
	if c {
		return a
	}
	return b
}

// parseCallArgs scans balanced top-level args starting at src[pos] (just
// after the '('), honoring nested parens and string literals. Returns the
// raw arg texts and the index just past ')'.
func parseCallArgs(src string, pos int) ([]string, int) {
	var args []string
	depth := 1
	start := pos
	for i := pos; i < len(src) && depth > 0; i++ {
		switch src[i] {
		case '"', '\'':
			j := skipJSString(src, i)
			i = j - 1
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				if a := strings.TrimSpace(src[start:i]); a != "" {
					args = append(args, a)
				}
				return args, i + 1
			}
		case ',':
			if depth == 1 {
				args = append(args, strings.TrimSpace(src[start:i]))
				start = i + 1
			}
		}
	}
	return args, len(src)
}

// skipJSString returns the index just past the closing quote of the string
// literal starting at src[i] (i indexes the opening ' or ").
func skipJSString(src string, i int) int {
	q := src[i]
	for i++; i < len(src); i++ {
		if src[i] == '\\' {
			i++
			continue
		}
		if src[i] == q {
			return i + 1
		}
	}
	return len(src)
}

// jsString decodes a JS string-literal arg ("..." or '...') with the
// common escapes; ok=false when the arg isn't a literal.
func jsString(arg string) (string, bool) {
	arg = strings.TrimSpace(arg)
	if len(arg) < 2 || (arg[0] != '"' && arg[0] != '\'') || arg[len(arg)-1] != arg[0] {
		return "", false
	}
	body := arg[1 : len(arg)-1]
	var b strings.Builder
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c != '\\' || i+1 >= len(body) {
			b.WriteByte(c)
			continue
		}
		i++
		switch body[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case 'u':
			if i+4 < len(body) {
				if v, err := strconv.ParseUint(body[i+1:i+5], 16, 32); err == nil {
					b.WriteRune(rune(v))
					i += 4
					continue
				}
			}
			b.WriteByte('u')
		default:
			b.WriteByte(body[i])
		}
	}
	return b.String(), true
}

// ------------------------------------------------------------- TS file ----

// readTranslations pulls the existing (ctx, source) → translation forms
// out of a whodat_<lang>.ts — reusing the runtime parser; unfinished and
// vanished entries are already dropped.
func readTranslations(path string) (map[string]map[string][]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c, err := i18n.ParseTS(b)
	if err != nil {
		return nil, err
	}
	return c.Messages(), nil
}

// writeTS emits catalog as a .ts document. When old is non-nil each
// message keeps its existing translation forms (numerus resized to the
// language's plural count); missing/empty translations are unfinished.
func writeTS(path, lang string, cat *catalog, old map[string]map[string][]string) error {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<!DOCTYPE TS>\n")
	b.WriteString("<TS version=\"2.1\"")
	if lang != "" {
		fmt.Fprintf(&b, " language=\"%s\"", xmlEsc(lang))
	}
	b.WriteString(">\n")
	nplur := i18n.PluralCount(lang)
	for _, ctx := range cat.order {
		b.WriteString("<context>\n")
		fmt.Fprintf(&b, "    <name>%s</name>\n", xmlEsc(ctx))
		for _, m := range cat.ctxs[ctx] {
			numerus := m.plural != ""
			b.WriteString("    <message")
			if numerus {
				b.WriteString(" numerus=\"yes\"")
			}
			b.WriteString(">\n")
			fmt.Fprintf(&b, "        <source>%s</source>\n", xmlEsc(m.source))
			if m.comment != "" {
				fmt.Fprintf(&b, "        <comment>%s</comment>\n", xmlEsc(m.comment))
			}
			forms := oldForms(old, ctx, m.source)
			if numerus {
				b.WriteString("        <translation")
				if !anyNonEmpty(forms) {
					b.WriteString(" type=\"unfinished\"")
				}
				b.WriteString(">\n")
				// English source: slot0 = singular source, slot1 = plural source.
				if lang == "" && len(forms) == 0 {
					forms = []string{m.source, m.plural}
				}
				for i := 0; i < max(1, nplur); i++ {
					f := ""
					if i < len(forms) {
						f = forms[i]
					}
					fmt.Fprintf(&b, "            <numerusform>%s</numerusform>\n", xmlEsc(f))
				}
				b.WriteString("        </translation>\n")
			} else {
				f := ""
				if len(forms) > 0 {
					f = forms[0]
				}
				b.WriteString("        <translation")
				if f == "" {
					b.WriteString(" type=\"unfinished\"")
				}
				fmt.Fprintf(&b, ">%s</translation>\n", xmlEsc(f))
			}
			b.WriteString("    </message>\n")
		}
		b.WriteString("</context>\n")
	}
	b.WriteString("</TS>\n")
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func oldForms(old map[string]map[string][]string, ctx, src string) []string {
	if old == nil {
		return nil
	}
	if m, ok := old[ctx]; ok {
		return m[src]
	}
	return nil
}

func anyNonEmpty(forms []string) bool {
	for _, f := range forms {
		if f != "" {
			return true
		}
	}
	return false
}

func xmlEsc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;",
		"\"", "&quot;", "'", "&apos;")
	return r.Replace(s)
}
