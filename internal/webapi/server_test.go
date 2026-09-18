package webapi

import (
	"bytes"
	"context"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/feytox/kabanbot/internal/app/settings"
	"github.com/feytox/kabanbot/internal/domain"
	"github.com/feytox/kabanbot/internal/llm"
	"github.com/feytox/kabanbot/internal/secrets"
	"github.com/feytox/kabanbot/internal/storage/sqlite"
)

type allAdmins struct{}

func (allAdmins) IsAdmin(context.Context, int64, int64) (bool, error) { return true, nil }

type noClients struct{}

func (noClients) Client(context.Context, domain.Provider) (llm.Client, error) { return nil, nil }
func (noClients) Invalidate(int64)                                            {}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	box, _ := secrets.New(bytes.Repeat([]byte{3}, 32))
	svc := settings.New(sqlite.NewModelStore(db, box), sqlite.NewChatStore(db), allAdmins{}, noClients{},
		0, slog.New(slog.DiscardHandler))
	static := fstest.MapFS{
		"index.html":    {Data: []byte("<html>app</html>")},
		"assets/app.js": {Data: []byte("js")},
	}
	srv := httptest.NewServer(New(svc, testToken, static, slog.New(slog.DiscardHandler)))
	t.Cleanup(srv.Close)
	return srv
}

func initDataFor(userID int64) string {
	v := url.Values{}
	v.Set("user", `{"id":`+strconv.FormatInt(userID, 10)+`,"first_name":"U"}`)
	v.Set("auth_date", strconv.FormatInt(time.Now().Unix(), 10))
	v.Set("hash", hex.EncodeToString(sign(v, testToken)))
	return v.Encode()
}

func do(t *testing.T, srv *httptest.Server, method, path string, userID int64, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), method, srv.URL+path, strings.NewReader(body))
	if userID != 0 {
		req.Header.Set("Authorization", "tma "+initDataFor(userID))
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestAPIRequiresInitData(t *testing.T) {
	srv := newTestServer(t)
	if code, _ := do(t, srv, http.MethodGet, "/api/providers", 0, ""); code != http.StatusUnauthorized {
		t.Fatalf("no auth: %d", code)
	}
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/api/me", nil)
	req.Header.Set("Authorization", "tma user=%7B%22id%22%3A1%7D&auth_date=1&hash=00")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("forged auth: %d", resp.StatusCode)
	}
}

func TestProviderLifecycleOverHTTP(t *testing.T) {
	srv := newTestServer(t)
	const key = "sk-very-secret-value-7777"

	code, body := do(t, srv, http.MethodPost, "/api/providers", 10,
		`{"kind":"openai","name":"mine","base_url":"https://api.example.com/v1","api_key":"`+key+`"}`)
	if code != http.StatusCreated || strings.Contains(body, key) || !strings.Contains(body, `"key_hint":"7777"`) {
		t.Fatalf("create: %d %s", code, body)
	}

	code, body = do(t, srv, http.MethodGet, "/api/providers", 10, "")
	if code != http.StatusOK || strings.Contains(body, key) || !strings.Contains(body, `"models":[]`) {
		t.Fatalf("list: %d %s", code, body)
	}

	// Validation errors carry a user-facing message.
	code, body = do(t, srv, http.MethodPost, "/api/providers", 10, `{"kind":"nope","name":"x","api_key":"k"}`)
	if code != http.StatusBadRequest || !strings.Contains(body, "Неизвестный тип") {
		t.Fatalf("invalid: %d %s", code, body)
	}
	// Another user's provider looks nonexistent.
	if code, _ = do(t, srv, http.MethodDelete, "/api/providers/1", 11, ""); code != http.StatusNotFound {
		t.Fatalf("foreign delete: %d", code)
	}
	if code, _ = do(t, srv, http.MethodDelete, "/api/providers/1", 10, ""); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if code, _ = do(t, srv, http.MethodPost, "/api/providers", 10, `{bad json`); code != http.StatusBadRequest {
		t.Fatalf("bad json: %d", code)
	}
}

func TestSPA(t *testing.T) {
	srv := newTestServer(t)
	for path, want := range map[string]string{
		"/":              "<html>app</html>",
		"/chats/-100":    "<html>app</html>",
		"/assets/app.js": "js",
	} {
		if code, body := do(t, srv, http.MethodGet, path, 0, ""); code != http.StatusOK || body != want {
			t.Errorf("%s: %d %q", path, code, body)
		}
	}
	if code, _ := do(t, srv, http.MethodGet, "/api/unknown", 10, ""); code != http.StatusNotFound {
		t.Errorf("unknown api route: %d", code)
	}
}
