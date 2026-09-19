package summary

import (
	"context"
	"errors"
	"iter"
	"log/slog"
	"strings"
	"testing"

	"github.com/feytox/kabanbot/internal/domain"
	"github.com/feytox/kabanbot/internal/llm"
)

type fakeHistory []domain.Message

func (f fakeHistory) Since(_ context.Context, _ int64, from int) ([]domain.Message, error) {
	var out []domain.Message
	for _, m := range f {
		if m.MessageID >= from {
			out = append(out, m)
		}
	}
	return out, nil
}

type fakeClient struct {
	got  llm.Request
	resp string
	err  error
}

func (f *fakeClient) Complete(_ context.Context, req llm.Request) (llm.Response, error) {
	f.got = req
	return llm.Response{Text: f.resp}, f.err
}

type fakeModels struct{ target llm.Target }

type fakeChats struct{ style string }

func (f fakeChats) Chat(context.Context, int64) (domain.Chat, error) {
	return domain.Chat{SummaryStyle: f.style}, nil
}

type fakeUsage struct{ got []domain.Usage }

func (f *fakeUsage) RecordUsage(_ context.Context, u domain.Usage) error {
	f.got = append(f.got, u)
	return nil
}

func newService(h History, m Models, style string) (*Service, *fakeUsage) {
	usage := &fakeUsage{}
	return New(h, m, fakeChats{style}, usage, "SYSTEM", slog.New(slog.DiscardHandler)), usage
}

func (f fakeModels) SummaryTarget(context.Context, int64) (llm.Target, error) { return f.target, nil }

func TestSummarize(t *testing.T) {
	history := fakeHistory{
		{MessageID: 1, Username: "alice", Text: "old"},
		{MessageID: 2, Username: "alice", Text: "Прив!"},
		{MessageID: 3, Username: "bob", Text: "Ку", ReplyTo: &domain.Quote{Username: "alice", Text: "Прив!"}},
	}
	temp := 0.2
	client := &fakeClient{resp: "  ## Итог\n- привет  "}
	svc, usage := newService(history, fakeModels{llm.Target{
		Client: client,
		Model:  domain.Model{ID: 3, Name: "m1", Params: domain.ModelParams{Temperature: &temp, MaxTokens: 500}},
	}}, "")

	got, err := svc.Summarize(t.Context(), 1, 42, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got != "## Итог\n- привет" {
		t.Errorf("summary = %q", got)
	}

	req := client.got
	if req.Model != "m1" || req.System != "SYSTEM" || req.MaxTokens != 500 || *req.Temperature != 0.2 {
		t.Errorf("request = %+v", req)
	}
	want := "alice: Прив!\nbob (replying to alice: \"Прив!\"): Ку"
	if len(req.Messages) != 1 || !strings.Contains(req.Messages[0].Content, want) {
		t.Errorf("prompt = %q, want it to contain %q", req.Messages[0].Content, want)
	}
	if strings.Contains(req.Messages[0].Content, "old") {
		t.Error("prompt contains messages before the start")
	}
	if len(usage.got) != 1 || usage.got[0].UserID != 42 || usage.got[0].ModelID != 3 || usage.got[0].Kind != domain.UsageSummary {
		t.Errorf("usage = %+v", usage.got)
	}
}

func TestSummarizeStyle(t *testing.T) {
	client := &fakeClient{resp: "ok"}
	svc, _ := newService(fakeHistory{{MessageID: 1, Username: "a", Text: "x"}},
		fakeModels{llm.Target{Client: client}}, "стихами")
	if _, err := svc.Summarize(t.Context(), 1, 1, 1); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(client.got.System, "SYSTEM") || !strings.Contains(client.got.System, "стихами") {
		t.Errorf("system = %q", client.got.System)
	}
}

func TestSummarizeNoHistory(t *testing.T) {
	svc, _ := newService(fakeHistory{}, fakeModels{}, "")
	if _, err := svc.Summarize(t.Context(), 1, 1, 5); !errors.Is(err, ErrNoHistory) {
		t.Fatalf("err = %v, want ErrNoHistory", err)
	}
}

func TestSummarizeLLMError(t *testing.T) {
	boom := errors.New("boom")
	svc, _ := newService(fakeHistory{{MessageID: 1, Username: "a", Text: "x"}},
		fakeModels{llm.Target{Client: &fakeClient{err: boom}}}, "")
	if _, err := svc.Summarize(t.Context(), 1, 1, 1); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want wrapped boom", err)
	}
}

func TestTranscriptTruncatesLongQuotes(t *testing.T) {
	long := strings.Repeat("я", maxQuoteLen+50)
	got := Transcript([]domain.Message{{Username: "a", Text: "ok", ReplyTo: &domain.Quote{Username: "b", Text: long}}})
	if strings.Contains(got, long) || !strings.Contains(got, "…") {
		t.Errorf("quote not truncated: %q", got)
	}
}

func (f *fakeClient) Stream(ctx context.Context, req llm.Request) iter.Seq2[llm.Chunk, error] {
	return llm.Single(f.Complete(ctx, req))
}
