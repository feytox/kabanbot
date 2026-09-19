package chat

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"

	"github.com/feytox/kabanbot/internal/app/settings"
	"github.com/feytox/kabanbot/internal/domain"
	"github.com/feytox/kabanbot/internal/llm"
)

const (
	toolGetPersonality   = "get_personality"
	toolSetPersonality   = "set_personality"
	toolResetPersonality = "reset_personality"
	toolSetSummaryStyle  = "set_summary_style"
)

var errBadArguments = errors.New("bad tool call")

// adminTools manage the chat's personality. They are offered only to the chat's admins.
var adminTools = []llm.Tool{
	{
		Name:        toolGetPersonality,
		Description: "Показать текущую личность бота и стиль пересказов в этом чате.",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	},
	{
		Name: toolSetPersonality,
		Description: "Заменить личность бота в этом чате: характер, манеру речи, роль. " +
			"Текст станет частью системных инструкций бота, пишите его от второго лица («Ты — …»).",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string", "description": "Новая личность целиком."},
			},
			"required": []string{"text"},
		},
	},
	{
		Name:        toolResetPersonality,
		Description: "Сбросить личность бота в этом чате к обычной.",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	},
	{
		Name:        toolSetSummaryStyle,
		Description: "Задать, как бот пишет пересказы /summary в этом чате. Пустая строка сбрасывает стиль.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"style": map[string]any{"type": "string", "description": "Указания для пересказов, например «стихами»."},
			},
			"required": []string{"style"},
		},
	},
}

// runTool carries out a tool call for the user. The model is not trusted: settings checks
// the user's rights again on every change.
func (s *Service) runTool(ctx context.Context, u settings.User, chatID int64, call llm.ToolCall) string {
	var args struct {
		Text  string `json:"text"`
		Style string `json:"style"`
	}
	if call.Arguments != "" {
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			return toolError(fmt.Errorf("%w: %w", errBadArguments, err))
		}
	}

	var err error
	switch call.Name {
	case toolGetPersonality:
		var c domain.Chat
		if c, err = s.personality(ctx, u, chatID); err == nil {
			return toolResult(map[string]any{"personality": c.Personality, "summary_style": c.SummaryStyle})
		}
	case toolSetPersonality:
		err = s.settings.SetPersonality(ctx, u, chatID, args.Text, domain.ViaTool)
	case toolResetPersonality:
		err = s.settings.SetPersonality(ctx, u, chatID, "", domain.ViaTool)
	case toolSetSummaryStyle:
		err = s.settings.SetSummaryStyle(ctx, u, chatID, args.Style)
	default:
		err = fmt.Errorf("%w: unknown tool %q", errBadArguments, call.Name)
	}
	if err != nil {
		s.log.InfoContext(ctx, "tool failed", "tool", call.Name, "chat_id", chatID, "user_id", u.ID, "err", err)
		return toolError(err)
	}
	s.log.InfoContext(ctx, "tool", "tool", call.Name, "chat_id", chatID, "user_id", u.ID)
	return toolResult(map[string]any{"ok": true})
}

// personality reads the chat for one of its admins.
func (s *Service) personality(ctx context.Context, u settings.User, chatID int64) (domain.Chat, error) {
	ok, err := s.settings.CanManage(ctx, u, chatID)
	if err != nil {
		return domain.Chat{}, err
	}
	if !ok {
		return domain.Chat{}, settings.ErrForbidden
	}
	return s.chats.Chat(ctx, chatID)
}

func toolResult(v map[string]any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// toolError tells the model what went wrong. Unexpected errors stay vague, since the model
// may repeat them in the chat.
func toolError(err error) string {
	msg := "не получилось, попробуйте позже"
	switch verr, ok := errors.AsType[*settings.ValidationError](err); {
	case ok:
		msg = verr.Msg
	case errors.Is(err, settings.ErrForbidden):
		msg = "менять это могут только администраторы чата"
	case errors.Is(err, errBadArguments):
		msg = err.Error()
	}
	return toolResult(map[string]any{"error": msg})
}
