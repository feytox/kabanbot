package domain

import (
	"regexp"
	"strings"
	"sync"
	"unicode"
)

// Trigger is a name the bot answers to in a group, besides mentions. The zero value is off.
type Trigger struct {
	// Text is a name matched as whole words, ignoring case and punctuation,
	// or a regular expression if Regex is set.
	Text  string
	Regex bool
}

// Enabled reports whether the trigger is set.
func (t Trigger) Enabled() bool { return t.Text != "" }

// Compile returns the trigger's regular expression, matched ignoring case.
func (t Trigger) Compile() (*regexp.Regexp, error) {
	return regexp.Compile("(?i)" + t.Text)
}

// compiled caches trigger expressions, since every group message is checked.
var compiled sync.Map // pattern -> *regexp.Regexp, or nil if invalid

// Match reports whether text calls the bot by this trigger.
func (t Trigger) Match(text string) bool {
	switch {
	case !t.Enabled():
		return false
	case t.Regex:
		re, ok := compiled.Load(t.Text)
		if !ok {
			r, err := t.Compile()
			if err != nil {
				r = nil
			}
			re, _ = compiled.LoadOrStore(t.Text, r)
		}
		r, _ := re.(*regexp.Regexp)
		return r != nil && r.MatchString(text)
	}
	name := Words(t.Text)
	if len(name) == 0 {
		return false
	}
	words := Words(text)
	for i := 0; i+len(name) <= len(words); i++ {
		if equalWords(words[i:i+len(name)], name) {
			return true
		}
	}
	return false
}

// Words splits text into lowercase words, dropping punctuation and other separators.
func Words(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func equalWords(a, b []string) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
