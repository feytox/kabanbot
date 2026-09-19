package telegram

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/feytox/kabanbot/internal/app/settings"
)

const user = 42

// testDialog asks for a name, an optional URL and a secret key, then saves them with save.
func testDialog(got *[]string, save func() error) *dialog {
	var name, url, key string
	field := func(dst *string) func(string) error {
		return func(v string) error {
			if v == "bad" {
				return &settings.ValidationError{Msg: "плохое значение"}
			}
			*dst = v
			return nil
		}
	}
	return &dialog{
		steps: []step{
			{prompt: "name?", accept: field(&name)},
			{prompt: "url?", optional: true, accept: field(&url)},
			{prompt: "key?", secret: true, accept: field(&key)},
		},
		finish: func(context.Context) (string, route, error) {
			if err := save(); err != nil {
				return "", route{}, err
			}
			*got = []string{name, url, key}
			return "saved", route{op: opProvider, id: 1}, nil
		},
	}
}

func mustAnswer(t *testing.T, ds *dialogs, text string) dialogReply {
	t.Helper()
	reply, ok, err := ds.answer(t.Context(), user, text)
	if !ok || err != nil {
		t.Fatalf("answer(%q) = %+v, %v, %v", text, reply, ok, err)
	}
	return reply
}

func promptOf(r dialogReply) string {
	if r.prompt == nil {
		return ""
	}
	return r.prompt.prompt
}

func TestDialogSteps(t *testing.T) {
	var ds dialogs
	var got []string
	if first := ds.start(user, testDialog(&got, func() error { return nil })); first.prompt != "name?" {
		t.Fatalf("first prompt = %q", first.prompt)
	}

	if r := mustAnswer(t, &ds, "  Мой  "); promptOf(r) != "url?" || r.deleteInput {
		t.Fatalf("after name: %+v", r)
	}
	if r := mustAnswer(t, &ds, "bad"); promptOf(r) != "url?" || r.notice != "плохое значение" {
		t.Fatalf("invalid url is asked again: %+v", r)
	}
	if r := mustAnswer(t, &ds, ""); promptOf(r) != "url?" || r.notice == "" {
		t.Fatalf("an empty message (e.g. a photo) is asked again: %+v", r)
	}
	if r := mustAnswer(t, &ds, skipInput); promptOf(r) != "key?" {
		t.Fatalf("optional step skipped with %q: %+v", skipInput, r)
	}
	if r := mustAnswer(t, &ds, "bad"); !r.deleteInput || promptOf(r) != "key?" {
		t.Fatalf("a rejected secret is still deleted and asked again: %+v", r)
	}
	r := mustAnswer(t, &ds, "sk-secret")
	if !r.deleteInput || r.prompt != nil || r.notice != "saved" || r.next == nil || *r.next != (route{op: opProvider, id: 1}) {
		t.Fatalf("last step: %+v", r)
	}
	if want := []string{"Мой", "", "sk-secret"}; len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("saved %q, want %q", got, want)
	}
	if _, ok, _ := ds.answer(t.Context(), user, "more"); ok {
		t.Error("a finished dialog still takes answers")
	}
}

func TestDialogFinishValidationErrorAsksAgain(t *testing.T) {
	var ds dialogs
	var got []string
	fail := true
	ds.start(user, testDialog(&got, func() error {
		if fail {
			fail = false
			return &settings.ValidationError{Msg: "ключ не подошёл"}
		}
		return nil
	}))
	mustAnswer(t, &ds, "name")
	mustAnswer(t, &ds, "-")
	if r := mustAnswer(t, &ds, "key1"); r.notice != "ключ не подошёл" || promptOf(r) != "key?" {
		t.Fatalf("validation error from finish: %+v", r)
	}
	if r := mustAnswer(t, &ds, "key2"); r.notice != "saved" || got[2] != "key2" {
		t.Fatalf("retry: %+v, saved %q", r, got)
	}
}

func TestDialogFinishFailureEndsDialog(t *testing.T) {
	var ds dialogs
	var got []string
	boom := errors.New("db down")
	ds.start(user, testDialog(&got, func() error { return boom }))
	mustAnswer(t, &ds, "name")
	mustAnswer(t, &ds, "-")
	if _, ok, err := ds.answer(t.Context(), user, "key"); !ok || !errors.Is(err, boom) {
		t.Fatalf("answer = %v, %v; want %v", ok, err, boom)
	}
	if _, ok, _ := ds.answer(t.Context(), user, "again"); ok {
		t.Error("the dialog should end after an unexpected error")
	}
}

func TestDialogCancel(t *testing.T) {
	var ds dialogs
	var got []string
	if ds.cancel(user) {
		t.Error("cancel without a dialog reported one")
	}
	ds.start(user, testDialog(&got, func() error { return nil }))
	mustAnswer(t, &ds, "name")
	if !ds.cancel(user) {
		t.Error("cancel did not find the dialog")
	}
	if _, ok, _ := ds.answer(t.Context(), user, "url"); ok {
		t.Error("a canceled dialog still takes answers")
	}
}

func TestDialogIsPerUser(t *testing.T) {
	var ds dialogs
	var got []string
	ds.start(user, testDialog(&got, func() error { return nil }))
	if _, ok, _ := ds.answer(t.Context(), user+1, "name"); ok {
		t.Error("another user's message was taken as an answer")
	}
}

func TestDialogTTL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var ds dialogs
		var got []string
		ds.start(user, testDialog(&got, func() error { return nil }))

		time.Sleep(dialogTTL - time.Second)
		mustAnswer(t, &ds, "name") // each answer extends the dialog

		time.Sleep(dialogTTL - time.Second)
		mustAnswer(t, &ds, "-")

		time.Sleep(dialogTTL + time.Second)
		if _, ok, _ := ds.answer(t.Context(), user, "key"); ok {
			t.Error("an expired dialog still takes answers")
		}
		if ds.cancel(user) {
			t.Error("cancel reported an expired dialog")
		}
	})
}

func TestDialogStartSweepsExpired(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var ds dialogs
		var got []string
		ds.start(user, testDialog(&got, func() error { return nil }))
		time.Sleep(dialogTTL + time.Second)
		ds.start(user+1, testDialog(&got, func() error { return nil }))
		if _, ok := ds.active[user]; ok {
			t.Error("an expired dialog was kept")
		}
	})
}
