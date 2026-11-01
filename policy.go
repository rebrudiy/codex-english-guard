package main

import (
	"fmt"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// rejectedRune returns the first character that violates the prompt policy.
// Math letter exceptions are intentionally global: Greek prose can pass.
func rejectedRune(prompt string) (rune, bool) {
	lastASCIIBase := false
	for _, r := range norm.NFC.String(prompt) {
		if unicode.Is(unicode.M, r) {
			if lastASCIIBase {
				return r, true
			}
			continue
		}
		lastASCIIBase = isASCIIEnglish(r)
		if unicode.IsLetter(r) && !lastASCIIBase && !isMathLetter(r) {
			return r, true
		}
	}
	return 0, false
}

func isASCIIEnglish(r rune) bool {
	return r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z'
}

func isMathLetter(r rune) bool {
	// Greek and Coptic, Mathematical Alphanumeric Symbols, Letterlike Symbols.
	return r >= 0x0370 && r <= 0x03FF ||
		r >= 0x1D400 && r <= 0x1D7FF ||
		r >= 0x2100 && r <= 0x214F
}

func rejectionReason(r rune) string {
	return fmt.Sprintf("English Guard found %q (U+%04X). Use English letters A-Z; numbers and symbols are allowed.", r, r)
}
