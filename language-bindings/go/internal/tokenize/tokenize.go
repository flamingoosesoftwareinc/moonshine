package tokenize

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// ErrInvalidUTF8 is returned when the input contains invalid UTF-8 byte sequences.
var ErrInvalidUTF8 = errors.New("casesplit: invalid UTF-8")

// Tokenize splits an identifier into lowercase word tokens.
// Returns [ErrInvalidUTF8] if the input is not valid UTF-8.
//
// Handles all common casing conventions:
//
//	"customerName"       → ["customer", "name"]
//	"customer_name"      → ["customer", "name"]
//	"customer-name"      → ["customer", "name"]
//	"CustomerName"       → ["customer", "name"]
//	"ForeignID"          → ["foreign", "id"]
//	"HTTPSServer"        → ["https", "server"]
//	"getAPIKey"          → ["get", "api", "key"]
//	"user_firstName"     → ["user", "first", "name"]
func Tokenize(s string) ([]string, error) {
	if !utf8.ValidString(s) {
		return nil, ErrInvalidUTF8
	}

	segments := splitDelimiters(s)

	var tokens []string

	for _, seg := range segments {
		for _, word := range splitCamelCase(seg) {
			lower := strings.ToLower(word)
			if lower != "" {
				tokens = append(tokens, lower)
			}
		}
	}

	return tokens, nil
}

type runeClass int

const (
	classOther runeClass = iota
	classLower
	classUpper
	classDigit
)

func classifyRune(r rune) runeClass {
	switch {
	case r >= 'a' && r <= 'z':
		return classLower
	case r >= 'A' && r <= 'Z':
		return classUpper
	case r >= '0' && r <= '9':
		return classDigit
	default:
		return classOther
	}
}

// splitCamelCase splits on ASCII case and letter/digit boundaries only.
// Non-ASCII runes are classOther and never trigger splits.
//
// Rules:
//   - lowercase/digit → uppercase: "getAPI" → "get|API"
//   - end of uppercase run into lowercase: "HTTPSServer" → "HTTPS|Server"
//   - letter ↔ digit transitions: "streetLine1" → "streetLine|1"
func splitCamelCase(s string) []string {
	runes := []rune(s)
	if len(runes) == 0 {
		return nil
	}

	classes := make([]runeClass, len(runes))
	for i, r := range runes {
		classes[i] = classifyRune(r)
	}

	var segments []string

	start := 0

	for i := 1; i < len(runes); i++ {
		prev := classes[i-1]
		curr := classes[i]

		// Uppercase after non-uppercase: "getAPI" → "get|API", "caféLatte" → "café|Latte"
		if prev != classUpper && curr == classUpper {
			segments = append(segments, string(runes[start:i]))
			start = i

			continue
		}

		// End of uppercase run into lowercase: "HTTPSServer" → "HTTPS|Server"
		if curr == classLower && prev == classUpper && i >= 2 && classes[i-2] == classUpper {
			if i-1 > start {
				segments = append(segments, string(runes[start:i-1]))
				start = i - 1
			}

			continue
		}

		// Digit boundary: split whenever entering or leaving a digit run
		if (prev != classDigit && curr == classDigit) ||
			(prev == classDigit && curr != classDigit) {
			segments = append(segments, string(runes[start:i]))
			start = i

			continue
		}
	}

	segments = append(segments, string(runes[start:]))

	return segments
}

// splitDelimiters splits on underscores, hyphens, and dots.
func splitDelimiters(s string) []string {
	var (
		segments []string
		current  strings.Builder
	)

	for _, r := range s {
		if r == '_' || r == '-' || r == '.' {
			if current.Len() > 0 {
				segments = append(segments, current.String())
				current.Reset()
			}
		} else {
			current.WriteRune(r)
		}
	}

	if current.Len() > 0 {
		segments = append(segments, current.String())
	}

	return segments
}
