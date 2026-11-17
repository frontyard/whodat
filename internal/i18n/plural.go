package i18n

import "strings"

// Plural rules select the <numerusform> slot for a count. The table follows
// gettext's plural-forms metadata (a CLDR subset): each language maps to
// (nplurals, rule); the default is the English family — 2 forms, n != 1.

// PluralCount reports how many numerus forms lang's catalog carries —
// used by the extractor to emit the right number of <numerusform> slots.
func PluralCount(lang string) int {
	np, _ := pluralRule(lang)
	return np
}

// PluralIndex returns the numerus slot for n under lang's rule.
func PluralIndex(lang string, n int) int {
	_, f := pluralRule(lang)
	return f(n)
}

func pluralRule(lang string) (int, func(int) int) {
	l := normalizeLangTag(lang)
	if i := strings.Index(l, "_"); i >= 0 {
		l = l[:i]
	}
	l = strings.ToLower(l)
	switch l {
	// One form — no singular/plural distinction.
	case "ja", "ko", "vi", "zh", "th", "id", "ms", "km", "lo", "my", "bo", "dz", "ka":
		return 1, func(int) int { return 0 }
	// French family — plural only for n > 1 (0 and 1 share the singular).
	case "fr", "pt", "tr", "fa", "hy", "az", "sq", "bn", "mr", "gu", "kn", "ml", "ta", "te", "am", "ak":
		return 2, func(n int) int {
			if n > 1 {
				return 1
			}
			return 0
		}
	// Russian family — three forms by last digit/last two digits.
	case "ru", "uk", "be", "hr", "sr", "bs", "cnr", "srp":
		return 3, func(n int) int {
			if n%10 == 1 && n%100 != 11 {
				return 0
			}
			if n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14) {
				return 1
			}
			return 2
		}
	case "pl":
		return 3, func(n int) int {
			if n == 1 {
				return 0
			}
			if n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14) {
				return 1
			}
			return 2
		}
	case "cs", "sk":
		return 3, func(n int) int {
			switch {
			case n == 1:
				return 0
			case n >= 2 && n <= 4:
				return 1
			default:
				return 2
			}
		}
	case "lt":
		return 3, func(n int) int {
			switch {
			case n%10 == 1 && n%100 != 11:
				return 0
			case n%10 >= 2 && (n%100 < 10 || n%100 >= 20):
				return 1
			default:
				return 2
			}
		}
	case "lv":
		return 3, func(n int) int {
			switch {
			case n%10 == 1 && n%100 != 11:
				return 0
			case n != 0:
				return 1
			default:
				return 2
			}
		}
	case "ro", "mo":
		return 3, func(n int) int {
			switch {
			case n == 1:
				return 0
			case n == 0 || (n%100 > 0 && n%100 < 20):
				return 1
			default:
				return 2
			}
		}
	case "sl":
		return 4, func(n int) int {
			switch {
			case n%100 == 1:
				return 0
			case n%100 == 2:
				return 1
			case n%100 == 3 || n%100 == 4:
				return 2
			default:
				return 3
			}
		}
	case "mk":
		return 2, func(n int) int {
			if n == 1 || n%10 == 1 {
				return 0
			}
			return 1
		}
	case "is":
		return 2, func(n int) int {
			if n%10 == 1 && n%100 != 11 {
				return 0
			}
			return 1
		}
	case "ga":
		return 5, func(n int) int {
			switch {
			case n == 1:
				return 0
			case n == 2:
				return 1
			case n >= 3 && n <= 6:
				return 2
			case n >= 7 && n <= 10:
				return 3
			default:
				return 4
			}
		}
	case "mt":
		return 4, func(n int) int {
			switch {
			case n == 1:
				return 0
			case n == 0 || (n%100 > 1 && n%100 < 11):
				return 1
			case n%100 > 10 && n%100 < 20:
				return 2
			default:
				return 3
			}
		}
	case "ar":
		return 6, func(n int) int {
			switch {
			case n == 0:
				return 0
			case n == 1:
				return 1
			case n == 2:
				return 2
			case n%100 >= 3 && n%100 <= 10:
				return 3
			case n%100 >= 11 && n%100 <= 99:
				return 4
			default:
				return 5
			}
		}
	case "cy":
		return 4, func(n int) int {
			switch {
			case n == 1:
				return 0
			case n == 2:
				return 1
			case n == 8 || n == 11:
				return 3
			default:
				return 2
			}
		}
	case "br":
		return 5, func(n int) int {
			switch {
			case n%10 == 1 && n%100 != 11 && n%100 != 71 && n%100 != 91:
				return 0
			case n%10 == 2 && n%100 != 12 && n%100 != 72 && n%100 != 92:
				return 1
			case (n%10 == 3 || n%10 == 4 || n%10 == 9) &&
				(n%100 < 10 || n%100 > 19) && (n%100 < 70 || n%100 > 79) && (n%100 < 90 || n%100 > 99):
				return 2
			case n != 0 && n%1000000 == 0:
				return 3
			default:
				return 4
			}
		}
	}
	// English family — the default for everything unlisted.
	return 2, func(n int) int {
		if n != 1 {
			return 1
		}
		return 0
	}
}
