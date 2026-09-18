package webapi

import (
	"github.com/feytox/kabanbot/internal/app/settings"
	"github.com/feytox/kabanbot/internal/domain"
)

// JSON shapes of the Mini App API. They never contain API keys or, for other users' models, URLs.

type meDTO struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username,omitzero"`
	IsOwner   bool   `json:"is_owner"`
}

type chatRefDTO struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

type modelDTO struct {
	ID          int64        `json:"id"`
	ProviderID  int64        `json:"provider_id"`
	Name        string       `json:"name"`
	DisplayName string       `json:"display_name"`
	Temperature *float64     `json:"temperature"`
	MaxTokens   int64        `json:"max_tokens"`
	Chats       []chatRefDTO `json:"chats"`
}

type providerDTO struct {
	ID      int64               `json:"id"`
	Kind    domain.ProviderKind `json:"kind"`
	Name    string              `json:"name"`
	BaseURL string              `json:"base_url"`
	KeyHint string              `json:"key_hint"`
	Shared  bool                `json:"shared"`
	Models  []modelDTO          `json:"models"`
}

type modelOptionDTO struct {
	ID           int64               `json:"id"`
	DisplayName  string              `json:"display_name"`
	Name         string              `json:"name"`
	ProviderName string              `json:"provider_name"`
	ProviderKind domain.ProviderKind `json:"provider_kind"`
	OwnerName    string              `json:"owner_name"`
	IsMine       bool                `json:"is_mine"`
	Shared       bool                `json:"shared"`
}

type featuresDTO struct {
	Summary    bool `json:"summary"`
	MentionAll bool `json:"mention_all"`
}

type chatDTO struct {
	ID           int64           `json:"id"`
	Title        string          `json:"title"`
	Enabled      bool            `json:"enabled"`
	Features     featuresDTO     `json:"features"`
	SummaryModel *modelOptionDTO `json:"summary_model"`
}

type providerInputDTO struct {
	Kind    domain.ProviderKind `json:"kind"`
	Name    string              `json:"name"`
	BaseURL string              `json:"base_url"`
	APIKey  string              `json:"api_key"`
	Shared  bool                `json:"shared"`
}

type modelInputDTO struct {
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name"`
	Temperature *float64 `json:"temperature"`
	MaxTokens   int64    `json:"max_tokens"`
}

type chatInputDTO struct {
	Enabled        bool        `json:"enabled"`
	Features       featuresDTO `json:"features"`
	SummaryModelID *int64      `json:"summary_model_id"`
}

type testResultDTO struct {
	Reply     string `json:"reply"`
	LatencyMS int64  `json:"latency_ms"`
}

type errorDTO struct {
	Error string `json:"error"`
}

func toProviderDTO(v settings.ProviderView) providerDTO {
	out := providerDTO{
		ID: v.ID, Kind: v.Kind, Name: v.Name, BaseURL: v.BaseURL, KeyHint: v.KeyHint, Shared: v.Shared,
		Models: make([]modelDTO, len(v.Models)),
	}
	for i, m := range v.Models {
		out.Models[i] = toModelDTO(m.Model, m.Chats)
	}
	return out
}

func toModelDTO(m domain.Model, chats []settings.ChatRef) modelDTO {
	out := modelDTO{
		ID: m.ID, ProviderID: m.ProviderID, Name: m.Name, DisplayName: m.DisplayName,
		Temperature: m.Params.Temperature, MaxTokens: m.Params.MaxTokens,
		Chats: make([]chatRefDTO, len(chats)),
	}
	for i, c := range chats {
		out.Chats[i] = chatRefDTO{ID: c.ID, Title: c.Title}
	}
	return out
}

func toModelOptionDTO(o domain.ModelOption, viewer int64) modelOptionDTO {
	return modelOptionDTO{
		ID: o.ID, DisplayName: o.DisplayName, Name: o.Name,
		ProviderName: o.ProviderName, ProviderKind: o.ProviderKind,
		OwnerName: o.OwnerName, IsMine: o.OwnerID == viewer, Shared: o.Shared,
	}
}

func toChatDTO(v settings.ChatView, viewer int64) chatDTO {
	out := chatDTO{
		ID: v.ID, Title: v.Title, Enabled: v.Settings.Enabled,
		Features: featuresDTO{Summary: v.Settings.Summary, MentionAll: v.Settings.MentionAll},
	}
	if v.SummaryModel != nil {
		out.SummaryModel = new(toModelOptionDTO(*v.SummaryModel, viewer))
	}
	return out
}

func (in providerInputDTO) toInput() settings.ProviderInput {
	return settings.ProviderInput{Kind: in.Kind, Name: in.Name, BaseURL: in.BaseURL, APIKey: in.APIKey, Shared: in.Shared}
}

func (in modelInputDTO) toInput() settings.ModelInput {
	return settings.ModelInput{
		Name: in.Name, DisplayName: in.DisplayName,
		Params: domain.ModelParams{Temperature: in.Temperature, MaxTokens: in.MaxTokens},
	}
}

func (in chatInputDTO) toInput() settings.ChatInput {
	return settings.ChatInput{
		Settings: domain.ChatSettings{
			Enabled:  in.Enabled,
			Features: domain.Features{Summary: in.Features.Summary, MentionAll: in.Features.MentionAll},
		},
		SummaryModelID: in.SummaryModelID,
	}
}
