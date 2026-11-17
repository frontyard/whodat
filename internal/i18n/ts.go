package i18n

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// Qt Linguist .ts format — the same files tools/i18nextract writes and
// Qt Linguist can edit. Only the subset the app produces/consumes is
// modeled: contexts, messages, numerus forms, and the type attribute.
type tsDoc struct {
	XMLName  xml.Name `xml:"TS"`
	Language string   `xml:"language,attr"`
	Contexts []tsCtx  `xml:"context"`
}

type tsCtx struct {
	Name string     `xml:"name"`
	Msgs []tsMsgXML `xml:"message"`
}

// tsMsgXML is the raw message node; translation holds either plain text
// or a list of <numerusform> forms.
type tsMsgXML struct {
	Source string `xml:"source"`
	Tr     tsTr   `xml:"translation"`
}

type tsTr struct {
	Type    string   `xml:"type,attr"`
	Text    string   `xml:",chardata"`
	Numerus []string `xml:"numerusform"`
}

// ParseTS decodes a .ts catalog. Entries marked unfinished, vanished or
// obsolete are dropped — they carry no usable translation.
func ParseTS(data []byte) (*Catalog, error) {
	var doc tsDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("i18n: parse .ts: %w", err)
	}
	c := &Catalog{
		Lang: normalizeLangTag(doc.Language),
		msgs: map[string]map[string][]string{},
	}
	for _, ctx := range doc.Contexts {
		for _, m := range ctx.Msgs {
			if m.Source == "" || skipType(m.Tr.Type) {
				continue
			}
			var forms []string
			if len(m.Tr.Numerus) > 0 {
				for _, f := range m.Tr.Numerus {
					forms = append(forms, strings.TrimSpace(f))
				}
				// All-empty numerus lists are unfinished in disguise.
				allEmpty := true
				for _, f := range forms {
					if f != "" {
						allEmpty = false
					}
				}
				if allEmpty {
					continue
				}
			} else {
				txt := strings.TrimSpace(m.Tr.Text)
				if txt == "" {
					continue
				}
				forms = []string{txt}
			}
			cm := c.msgs[ctx.Name]
			if cm == nil {
				cm = map[string][]string{}
				c.msgs[ctx.Name] = cm
			}
			cm[m.Source] = forms
		}
	}
	return c, nil
}

func skipType(t string) bool {
	switch t {
	case "unfinished", "vanished", "obsolete":
		return true
	}
	return false
}

// normalizeLangTag makes file/attr locale codes comparable ("de-AT" → "de_AT").
func normalizeLangTag(l string) string {
	return strings.ReplaceAll(strings.TrimSpace(l), "-", "_")
}
