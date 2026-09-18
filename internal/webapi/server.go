// Package webapi serves the Mini App and its JSON API.
package webapi

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/feytox/kabanbot/internal/app/settings"
	"github.com/feytox/kabanbot/internal/domain"
)

const (
	// initDataMaxAge bounds how long a Mini App session stays valid after launch.
	initDataMaxAge = 24 * time.Hour
	maxBodyBytes   = 64 << 10
)

// Server is the Mini App HTTP handler.
type Server struct {
	svc      *settings.Service
	botToken string
	static   fs.FS // built Mini App; may be nil
	log      *slog.Logger
	now      func() time.Time
	mux      *http.ServeMux
}

// New creates the handler. static holds the built Mini App and may be nil.
func New(svc *settings.Service, botToken string, static fs.FS, log *slog.Logger) *Server {
	s := &Server{svc: svc, botToken: botToken, static: static, log: log, now: time.Now, mux: http.NewServeMux()}

	s.mux.HandleFunc("GET /api/me", s.auth(s.me))
	s.mux.HandleFunc("GET /api/providers", s.auth(s.listProviders))
	s.mux.HandleFunc("POST /api/providers", s.auth(s.createProvider))
	s.mux.HandleFunc("PUT /api/providers/{id}", s.auth(s.updateProvider))
	s.mux.HandleFunc("DELETE /api/providers/{id}", s.auth(s.deleteProvider))
	s.mux.HandleFunc("POST /api/providers/{id}/models", s.auth(s.createModel))
	s.mux.HandleFunc("PUT /api/models/{id}", s.auth(s.updateModel))
	s.mux.HandleFunc("DELETE /api/models/{id}", s.auth(s.deleteModel))
	s.mux.HandleFunc("POST /api/models/{id}/test", s.auth(s.testModel))
	s.mux.HandleFunc("DELETE /api/models/{id}/chats/{chat}", s.auth(s.unbindModel))
	s.mux.HandleFunc("GET /api/models/usable", s.auth(s.usableModels))
	s.mux.HandleFunc("GET /api/chats", s.auth(s.listChats))
	s.mux.HandleFunc("GET /api/chats/{chat}", s.auth(s.getChat))
	s.mux.HandleFunc("PUT /api/chats/{chat}", s.auth(s.updateChat))
	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, errorDTO{Error: "not found"})
	})
	s.mux.Handle("/", s.spa())
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	s.mux.ServeHTTP(w, r)
}

type handler func(w http.ResponseWriter, r *http.Request, u settings.User) error

// auth authenticates the request by its "Authorization: tma <initData>" header.
func (s *Server) auth(h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "tma ")
		if !ok {
			writeJSON(w, http.StatusUnauthorized, errorDTO{Error: "missing init data"})
			return
		}
		data, err := parseInitData(raw, s.botToken, initDataMaxAge, s.now())
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, errorDTO{Error: "invalid init data"})
			return
		}
		u := settings.User{ID: data.User.ID, Username: data.User.Username, FirstName: data.User.FirstName}
		if err := s.svc.Seen(r.Context(), u); err != nil {
			s.log.WarnContext(r.Context(), "record user", "err", err)
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		if err := h(w, r, u); err != nil {
			s.writeError(r.Context(), w, err)
		}
	}
}

func (s *Server) me(w http.ResponseWriter, _ *http.Request, u settings.User) error {
	writeJSON(w, http.StatusOK, meDTO{ID: u.ID, FirstName: u.FirstName, Username: u.Username, IsOwner: s.svc.IsOwner(u)})
	return nil
}

func (s *Server) listProviders(w http.ResponseWriter, r *http.Request, u settings.User) error {
	views, err := s.svc.Providers(r.Context(), u)
	if err != nil {
		return err
	}
	out := make([]providerDTO, len(views))
	for i, v := range views {
		out[i] = toProviderDTO(v)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) createProvider(w http.ResponseWriter, r *http.Request, u settings.User) error {
	var in providerInputDTO
	if err := readJSON(r, &in); err != nil {
		return err
	}
	p, err := s.svc.CreateProvider(r.Context(), u, in.toInput())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, toProviderDTO(settings.ProviderView{Provider: p}))
	return nil
}

func (s *Server) updateProvider(w http.ResponseWriter, r *http.Request, u settings.User) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var in providerInputDTO
	if err := readJSON(r, &in); err != nil {
		return err
	}
	p, err := s.svc.UpdateProvider(r.Context(), u, id, in.toInput())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, toProviderDTO(settings.ProviderView{Provider: p}))
	return nil
}

func (s *Server) deleteProvider(w http.ResponseWriter, r *http.Request, u settings.User) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	if err := s.svc.DeleteProvider(r.Context(), u, id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) createModel(w http.ResponseWriter, r *http.Request, u settings.User) error {
	providerID, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var in modelInputDTO
	if err := readJSON(r, &in); err != nil {
		return err
	}
	m, err := s.svc.CreateModel(r.Context(), u, providerID, in.toInput())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, toModelDTO(m, nil))
	return nil
}

func (s *Server) updateModel(w http.ResponseWriter, r *http.Request, u settings.User) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var in modelInputDTO
	if err := readJSON(r, &in); err != nil {
		return err
	}
	m, err := s.svc.UpdateModel(r.Context(), u, id, in.toInput())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, toModelDTO(m, nil))
	return nil
}

func (s *Server) deleteModel(w http.ResponseWriter, r *http.Request, u settings.User) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	if err := s.svc.DeleteModel(r.Context(), u, id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) testModel(w http.ResponseWriter, r *http.Request, u settings.User) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	reply, latency, err := s.svc.TestModel(r.Context(), u, id)
	if err != nil {
		return testError{err}
	}
	writeJSON(w, http.StatusOK, testResultDTO{Reply: reply, LatencyMS: latency.Milliseconds()})
	return nil
}

// testError is a failed model test. The model belongs to the caller,
// so the upstream error is theirs to see.
type testError struct{ error }

func (e testError) Unwrap() error { return e.error }

func (s *Server) unbindModel(w http.ResponseWriter, r *http.Request, u settings.User) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	chatID, err := pathID(r, "chat")
	if err != nil {
		return err
	}
	if err := s.svc.UnbindModel(r.Context(), u, id, chatID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) usableModels(w http.ResponseWriter, r *http.Request, u settings.User) error {
	opts, err := s.svc.UsableModels(r.Context(), u)
	if err != nil {
		return err
	}
	out := make([]modelOptionDTO, len(opts))
	for i, o := range opts {
		out[i] = toModelOptionDTO(o, u.ID)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) listChats(w http.ResponseWriter, r *http.Request, u settings.User) error {
	views, err := s.svc.Chats(r.Context(), u)
	if err != nil {
		return err
	}
	out := make([]chatDTO, len(views))
	for i, v := range views {
		out[i] = toChatDTO(v, u.ID)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) getChat(w http.ResponseWriter, r *http.Request, u settings.User) error {
	id, err := pathID(r, "chat")
	if err != nil {
		return err
	}
	v, err := s.svc.Chat(r.Context(), u, id)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, toChatDTO(v, u.ID))
	return nil
}

func (s *Server) updateChat(w http.ResponseWriter, r *http.Request, u settings.User) error {
	id, err := pathID(r, "chat")
	if err != nil {
		return err
	}
	var in chatInputDTO
	if err := readJSON(r, &in); err != nil {
		return err
	}
	v, err := s.svc.UpdateChat(r.Context(), u, id, in.toInput())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, toChatDTO(v, u.ID))
	return nil
}

// badRequest is a client error whose message is safe to return.
type badRequest string

func (e badRequest) Error() string { return string(e) }

func pathID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil {
		return 0, badRequest("bad " + name)
	}
	return id, nil
}

func readJSON(r *http.Request, v any) error {
	if err := json.UnmarshalRead(r.Body, v); err != nil {
		return badRequest("bad JSON body")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.MarshalWrite(w, v)
}

func (s *Server) writeError(ctx context.Context, w http.ResponseWriter, err error) {
	verr, isValidation := errors.AsType[*settings.ValidationError](err)
	bad, isBad := errors.AsType[badRequest](err)
	testErr, isTest := errors.AsType[testError](err)
	switch {
	case isValidation:
		writeJSON(w, http.StatusBadRequest, errorDTO{Error: verr.Msg})
	case isBad:
		writeJSON(w, http.StatusBadRequest, errorDTO{Error: bad.Error()})
	case errors.Is(err, domain.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorDTO{Error: "Не найдено"})
	case errors.Is(err, settings.ErrForbidden):
		writeJSON(w, http.StatusForbidden, errorDTO{Error: "Нужны права администратора чата"})
	case errors.Is(err, domain.ErrNoMasterKey):
		writeJSON(w, http.StatusServiceUnavailable, errorDTO{Error: "На сервере не настроен MASTER_KEY: хранить ключи провайдеров нельзя"})
	case isTest:
		writeJSON(w, http.StatusBadGateway, errorDTO{Error: testErr.Error()})
	default:
		s.log.ErrorContext(ctx, "api", "err", err)
		writeJSON(w, http.StatusInternalServerError, errorDTO{Error: "Внутренняя ошибка"})
	}
}

// spa serves the built Mini App, falling back to index.html for client-side routes.
func (s *Server) spa() http.Handler {
	if s.static == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "Mini App is not built", http.StatusNotFound)
		})
	}
	files := http.FileServerFS(s.static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if _, err := fs.Stat(s.static, path); err == nil {
				if strings.HasPrefix(path, "assets/") {
					// Vite puts content hashes into asset names.
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, s.static, "index.html")
	})
}
