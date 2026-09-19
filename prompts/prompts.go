// Package prompts embeds the system prompts used for LLM requests.
package prompts

import (
	_ "embed"
	"strings"
)

//go:embed summary.md
var summary string

//go:embed chat.md
var chat string

// Summary returns the system prompt for chat summarization.
func Summary() string { return strings.TrimSpace(summary) }

// Chat returns the system prompt for talking with people.
func Chat() string { return strings.TrimSpace(chat) }
