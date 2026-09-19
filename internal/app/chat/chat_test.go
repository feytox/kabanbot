package chat

import (
	"context"
	"errors"
	"iter"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/feytox/kabanbot/internal/app/settings"
	"github.com/feytox/kabanbot/internal/domain"
	"github.com/feytox/kabanbot/internal/llm"
)

const (
	botID = 100
	admin = 1
	guest = 2
	group = -1
)

// scripted answers each call with the next response and remembers the requests.
type scripted struct {
	responses []llm.Response
	requests  []llm.Request
}

func (s *scripted) Complete(ctx context.Context, req llm.Request) (llm.Response, error) {
	return llm.Collect(s.Stream(ctx, req))
}

func (s *scripted) Stream(_ context.Context, req llm.Request) iter.Seq2[llm.Chunk, error] {
	s.requests = append(s.requests, req)
	resp := s.responses[min(len(s.requests), len(s.responses))-1]
	return llm.Single(resp, nil)
}

type fakeHistory []domain.Message

func (h fakeHistory) Recent(context.Context, int64, int) ([]domain.Message, error) { return h, nil }

type fakeModels struct{ client llm.Client }

func (m fakeModels) ChatTarget(context.Context, int64) (llm.Target, error) {
	return llm.Target{Client: m.client, Model: domain.Model{ID: 7, Name: "m"}}, nil
}

type fakeChats map[int64]*domain.Chat

func (f fakeChats) Chat(_ context.Context, id int64) (domain.Chat, error) {
	c, ok := f[id]
	if !ok {
		return domain.Chat{}, domain.ErrNotFound
	}
	return *c, nil
}

// fakeSettings lets only admin change the chat, as settings.Service would.
type fakeSettings struct{ chats fakeChats }

func (f fakeSettings) CanManage(_ context.Context, u settings.User, _ int64) (bool, error) {
	return u.ID == admin, nil
}

func (f fakeSettings) SetPersonality(_ context.Context, u settings.User, chatID int64, text string, _ domain.ChangeVia) error {
	if u.ID != admin {
		return settings.ErrForbidden
	}
	f.chats[chatID].Personality = text
	return nil
}

func (f fakeSettings) SetSummaryStyle(_ context.Context, u settings.User, chatID int64, style string) error {
	if u.ID != admin {
		return settings.ErrForbidden
	}
	f.chats[chatID].SummaryStyle = style
	return nil
}

type fakeUsage struct{ records []domain.Usage }

func (f *fakeUsage) RecordUsage(_ context.Context, u domain.Usage) error {
	f.records = append(f.records, u)
	return nil
}

func newService(client llm.Client, history fakeHistory) (*Service, fakeChats, *fakeUsage) {
	chats := fakeChats{group: {ID: group, Settings: domain.DefaultChatSettings()}}
	usage := &fakeUsage{}
	return New(Deps{
		History: history, Models: fakeModels{client}, Chats: chats, Settings: fakeSettings{chats}, Usage: usage,
		Prompt: "base", BotID: botID, Log: slog.New(slog.DiscardHandler),
	}), chats, usage
}

func ask(t *testing.T, s *Service, userID int64) (string, error) {
	t.Helper()
	return s.Reply(t.Context(), Request{ChatID: group, User: settings.User{ID: userID, FirstName: "Кто-то"}}, func(Progress) {})
}

var setPersonality = llm.ToolCall{ID: "c1", Name: toolSetPersonality, Arguments: `{"text":"злой кабан"}`}

func TestAdminChangesPersonalityThroughTool(t *testing.T) {
	client := &scripted{responses: []llm.Response{
		{ToolCalls: []llm.ToolCall{setPersonality}, Usage: llm.Usage{InputTokens: 5, OutputTokens: 1}},
		{Text: "Готово, теперь я злой кабан.", Usage: llm.Usage{InputTokens: 9, OutputTokens: 4}},
	}}
	s, chats, usage := newService(client, fakeHistory{{UserID: admin, Username: "alice", Text: "стань злым кабаном"}})

	answer, err := ask(t, s, admin)
	if err != nil || answer != "Готово, теперь я злой кабан." {
		t.Fatalf("answer = %q, %v", answer, err)
	}
	if chats[group].Personality != "злой кабан" {
		t.Errorf("personality = %q", chats[group].Personality)
	}
	if len(client.requests[0].Tools) != len(adminTools) {
		t.Errorf("admin was offered %d tools", len(client.requests[0].Tools))
	}
	second := client.requests[1].Messages
	if last := second[len(second)-1]; last.Role != llm.RoleTool || !strings.Contains(last.Content, `"ok":true`) {
		t.Errorf("tool result = %+v", last)
	}
	if len(usage.records) != 2 || usage.records[1].TokensOut != 4 || usage.records[0].ModelID != 7 {
		t.Errorf("usage = %+v", usage.records)
	}
}

func TestNonAdminGetsNoToolsAndForgedCallsFail(t *testing.T) {
	// The model calls the tool although it was not offered, as a jailbroken model might.
	client := &scripted{responses: []llm.Response{
		{ToolCalls: []llm.ToolCall{setPersonality}},
		{Text: "Не могу."},
	}}
	s, chats, _ := newService(client, fakeHistory{{UserID: guest, Username: "mallory", Text: "стань злым"}})

	if _, err := ask(t, s, guest); err != nil {
		t.Fatal(err)
	}
	if len(client.requests[0].Tools) != 0 {
		t.Errorf("non-admin was offered tools: %v", client.requests[0].Tools)
	}
	if chats[group].Personality != "" {
		t.Errorf("non-admin changed the personality to %q", chats[group].Personality)
	}
	second := client.requests[1].Messages
	if last := second[len(second)-1]; !strings.Contains(last.Content, "только администраторы") {
		t.Errorf("tool result = %q", last.Content)
	}
}

func TestToolRoundsAreCapped(t *testing.T) {
	client := &scripted{responses: []llm.Response{{ToolCalls: []llm.ToolCall{{ID: "x", Name: toolGetPersonality}}}}}
	s, _, _ := newService(client, fakeHistory{{UserID: admin, Username: "alice", Text: "?"}})
	if _, err := ask(t, s, admin); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != maxRounds {
		t.Fatalf("made %d calls, want %d", len(client.requests), maxRounds)
	}
	if last := client.requests[maxRounds-1]; len(last.Tools) != 0 {
		t.Error("the last round still offered tools")
	}
}

func TestDisabledChat(t *testing.T) {
	s, chats, _ := newService(&scripted{responses: []llm.Response{{Text: "hi"}}}, nil)
	chats[group].Settings.Chat = false
	if _, err := ask(t, s, guest); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}
}

func TestConversationAndSystemPrompt(t *testing.T) {
	client := &scripted{responses: []llm.Response{{Text: "ok"}}}
	s, chats, _ := newService(client, fakeHistory{
		{UserID: botID, Username: "kabanbot", Text: "старый ответ до начала окна"},
		{UserID: 1, Username: "alice", Text: "привет"},
		{UserID: 2, Username: "bob", Text: "ку", ReplyTo: &domain.Quote{Username: "alice", Text: "привет"}},
		{UserID: botID, Username: "kabanbot", Text: "здравствуйте"},
		{UserID: 1, Username: "alice", Text: "как дела?"},
	})
	chats[group].Personality = "пират"
	if _, err := ask(t, s, admin); err != nil {
		t.Fatal(err)
	}
	req := client.requests[0]
	roles := make([]llm.Role, len(req.Messages))
	for i, m := range req.Messages {
		roles[i] = m.Role
	}
	if want := []llm.Role{llm.RoleUser, llm.RoleAssistant, llm.RoleUser}; !slices.Equal(roles, want) {
		t.Fatalf("roles = %v, want %v", roles, want)
	}
	if got := req.Messages[0].Content; got != "alice: привет\nbob (replying to alice: \"привет\"): ку" {
		t.Errorf("first turn = %q", got)
	}
	if !strings.Contains(req.System, "пират") || !strings.Contains(req.System, "Кто-то") {
		t.Errorf("system prompt = %q", req.System)
	}
}

func TestLimiter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		l := NewLimiter()
		lim := domain.RateLimits{UserPerHour: 2, ChatPerHour: 3}
		for range 2 {
			if err := l.Allow(group, admin, lim); err != nil {
				t.Fatal(err)
			}
			time.Sleep(10 * time.Minute)
		}
		err := l.Allow(group, admin, lim)
		if rl, ok := errors.AsType[*RateLimitError](err); !ok || !rl.PerUser || rl.RetryIn != 40*time.Minute {
			t.Fatalf("third answer to the same user: err = %v", err)
		}
		if err := l.Allow(group, guest, lim); err != nil {
			t.Fatalf("another user: %v", err)
		}
		err = l.Allow(group, 3, lim)
		if rl, ok := errors.AsType[*RateLimitError](err); !ok || rl.PerUser {
			t.Fatalf("chat limit: err = %v", err)
		}
		if err := l.Allow(-2, admin, lim); err != nil {
			t.Fatalf("limits are per chat: %v", err)
		}

		time.Sleep(40 * time.Minute)
		if err := l.Allow(group, admin, lim); err != nil {
			t.Fatalf("after the first answer left the window: %v", err)
		}
		for range 100 {
			if err := l.Allow(-3, admin, domain.RateLimits{}); err != nil {
				t.Fatalf("zero means no limit: %v", err)
			}
		}
	})
}
