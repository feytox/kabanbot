package settings

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/feytox/kabanbot/internal/domain"
)

const (
	// MaxPersonality bounds a chat's personality, which goes into every chat request.
	MaxPersonality = 2000
	// MaxSummaryStyle bounds a chat's summary style.
	MaxSummaryStyle = 500
	// historyLimit is how many personality changes are kept on view.
	historyLimit = 10
)

// CanManage reports whether the user may change the chat's settings, e.g. its personality.
func (s *Service) CanManage(ctx context.Context, u User, chatID int64) (bool, error) {
	_, err := s.adminChat(ctx, u, chatID)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, ErrForbidden):
		return false, nil
	}
	return false, err
}

// ValidatePersonality checks a personality text. Empty resets it.
func ValidatePersonality(text string) error {
	if utf8.RuneCountInString(strings.TrimSpace(text)) > MaxPersonality {
		return invalid("Личность должна быть не длиннее %d символов", MaxPersonality)
	}
	return nil
}

// ValidateSummaryStyle checks a summary style. Empty resets it.
func ValidateSummaryStyle(style string) error {
	if utf8.RuneCountInString(strings.TrimSpace(style)) > MaxSummaryStyle {
		return invalid("Стиль пересказов должен быть не длиннее %d символов", MaxSummaryStyle)
	}
	return nil
}

// maxTrigger bounds a trigger name or pattern.
const maxTrigger = 200

// ValidateTrigger checks the name or pattern the bot answers to. An empty one is off.
func ValidateTrigger(t domain.Trigger) error {
	switch {
	case !t.Enabled():
		return nil
	case utf8.RuneCountInString(t.Text) > maxTrigger:
		return invalid("Слишком длинно: не больше %d символов", maxTrigger)
	case t.Regex:
		if _, err := t.Compile(); err != nil {
			return invalid("Это не регулярное выражение: %v", err)
		}
		if t.Match("") {
			return invalid("Такое выражение совпадает с любым сообщением")
		}
	case len(domain.Words(t.Text)) == 0:
		return invalid("В имени должны быть буквы или цифры")
	}
	return nil
}

// SetPersonality changes the chat's personality; empty text resets it. Only the chat's admins may.
func (s *Service) SetPersonality(ctx context.Context, u User, chatID int64, text string, via domain.ChangeVia) error {
	if _, err := s.adminChat(ctx, u, chatID); err != nil {
		return err
	}
	if err := ValidatePersonality(text); err != nil {
		return err
	}
	return s.chats.SetPersonality(ctx, chatID, strings.TrimSpace(text), u.ID, via)
}

// PersonalityHistory lists the chat's latest personality changes, newest first.
func (s *Service) PersonalityHistory(ctx context.Context, u User, chatID int64) ([]domain.PersonalityChange, error) {
	if _, err := s.adminChat(ctx, u, chatID); err != nil {
		return nil, err
	}
	return s.chats.PersonalityHistory(ctx, chatID, historyLimit)
}

// RevertPersonality brings back the personality as it was after an earlier change.
func (s *Service) RevertPersonality(ctx context.Context, u User, chatID, changeID int64) error {
	change, err := s.chats.PersonalityChange(ctx, changeID)
	if err != nil {
		return err
	}
	if change.ChatID != chatID {
		return ErrNotFound
	}
	return s.SetPersonality(ctx, u, chatID, change.Text, domain.ViaMenu)
}

// SetSummaryStyle changes how summaries are written in the chat; empty resets it.
func (s *Service) SetSummaryStyle(ctx context.Context, u User, chatID int64, style string) error {
	if _, err := s.adminChat(ctx, u, chatID); err != nil {
		return err
	}
	if err := ValidateSummaryStyle(style); err != nil {
		return err
	}
	return s.chats.SetSummaryStyle(ctx, chatID, strings.TrimSpace(style), u.ID)
}

// Stats is a chat's model usage.
type Stats struct {
	Day, Month domain.UsageTotals
	// TopUsers are the month's most active users.
	TopUsers []domain.UserUsage
}

// Stats returns the chat's model usage for the last day and the last 30 days.
func (s *Service) Stats(ctx context.Context, u User, chatID int64) (Stats, error) {
	if _, err := s.adminChat(ctx, u, chatID); err != nil {
		return Stats{}, err
	}
	now := time.Now()
	var out Stats
	var err error
	if out.Day, err = s.usage.UsageTotals(ctx, chatID, now.Add(-24*time.Hour)); err != nil {
		return Stats{}, err
	}
	month := now.AddDate(0, 0, -30)
	if out.Month, err = s.usage.UsageTotals(ctx, chatID, month); err != nil {
		return Stats{}, err
	}
	if out.TopUsers, err = s.usage.UsageByUser(ctx, chatID, month, 5); err != nil {
		return Stats{}, err
	}
	return out, nil
}
