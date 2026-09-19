package mention

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/feytox/kabanbot/internal/domain"
)

type fakeUsers []domain.User

func (f fakeUsers) Users(context.Context, int64) ([]domain.User, error) { return f, nil }

type fakeAdmins struct {
	users []domain.User
	err   error
}

func (f fakeAdmins) Admins(context.Context, int64) ([]domain.User, error) { return f.users, f.err }

func TestTargets(t *testing.T) {
	svc := New(
		fakeUsers{{ID: 1, Name: "author"}, {ID: 2, Name: "bob"}},
		fakeAdmins{users: []domain.User{{ID: 2, Name: "Bob Admin"}, {ID: 3, Name: "Carol"}}},
		slog.New(slog.DiscardHandler),
	)
	got, err := svc.Targets(t.Context(), -100, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.User{{ID: 2, Name: "bob"}, {ID: 3, Name: "Carol"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("targets = %+v, want %+v", got, want)
	}
}

func TestTargetsToleratesAdminError(t *testing.T) {
	svc := New(fakeUsers{{ID: 2, Name: "bob"}}, fakeAdmins{err: errors.New("no rights")}, slog.New(slog.DiscardHandler))
	got, err := svc.Targets(t.Context(), -100, 1)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
}
