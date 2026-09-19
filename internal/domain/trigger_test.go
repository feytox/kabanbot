package domain

import "testing"

func TestTriggerWords(t *testing.T) {
	kaban := Trigger{Text: "Кабан"}
	for text, want := range map[string]bool{
		"кабан, привет":      true,
		"Эй, КАБАН!":         true,
		"(кабан)":            true,
		"кабанчик, привет":   false,
		"мой кабан-бот":      true,
		"никаких упоминаний": false,
		"":                   false,
	} {
		if got := kaban.Match(text); got != want {
			t.Errorf("Match(%q) = %v, want %v", text, got, want)
		}
	}
	two := Trigger{Text: "Кабан Бот"}
	if !two.Match("эй, кабан бот!") || !two.Match("кабан, бот?") || two.Match("бот кабан") {
		t.Error("a name of two words must match them in a row")
	}
	if (Trigger{}).Match("кабан") || (Trigger{Text: "!!!"}).Match("!!!") {
		t.Error("an empty trigger matched")
	}
}

func TestTriggerRegex(t *testing.T) {
	re := Trigger{Text: `^кабан(чик)?[,!]`, Regex: true}
	if !re.Match("Кабанчик, как дела") || re.Match("мой кабанчик") {
		t.Error("regex trigger")
	}
	if (Trigger{Text: "(", Regex: true}).Match("(") {
		t.Error("an invalid pattern matched")
	}
}
