package telegram

import (
	"cmp"
	"context"
	"fmt"
	"html"
	"slices"
	"strconv"
	"strings"

	"github.com/mymmrac/telego"

	"github.com/feytox/kabanbot/internal/app/settings"
	"github.com/feytox/kabanbot/internal/domain"
)

// startParamChatPrefix opens a chat's settings in the private chat, e.g. t.me/<bot>?start=g_-100123.
// Text for a group, such as its personality, is typed there rather than in the group.
const startParamChatPrefix = "g_"

// Choices offered by the limit buttons, in the order they cycle. Zero means no limit.
var (
	userLimitSteps = []int{5, 10, 20, 50, 0}
	chatLimitSteps = []int{20, 60, 150, 500, 0}
)

// handleChatRoute performs routes about one chat: a group or the user's private chat.
func (m *menu) handleChatRoute(ctx context.Context, v view, r route) (outcome, error) {
	switch r.op {
	case opGroup:
		return show(m.chatScreen(ctx, v, r.id))
	case opGroupToggle:
		return showWith("Сохранено")(m.update(ctx, v, r.id, func(in *settings.ChatInput) { toggle(&in.Settings, r.word) }))
	case opGroupModels:
		return show(m.modelPicker(ctx, v, r.id, false))
	case opGroupModel:
		return showWith("Модель выбрана")(m.update(ctx, v, r.id, func(in *settings.ChatInput) { in.SummaryModelID = optionalID(r.id2) }))
	case opChatModels:
		return show(m.modelPicker(ctx, v, r.id, true))
	case opChatModel:
		return showWith("Модель выбрана")(m.update(ctx, v, r.id, func(in *settings.ChatInput) { in.ChatModelID = optionalID(r.id2) }))

	case opPersonality:
		return show(m.personality(ctx, v, r.id))
	case opPersonalityEdit:
		return m.editPersonality(ctx, v, r.id)
	case opPersonalityReset:
		if err := m.svc.SetPersonality(ctx, v.user, r.id, "", domain.ViaMenu); err != nil {
			return outcome{}, err
		}
		return showWith("Личность сброшена")(m.personality(ctx, v, r.id))
	case opPersonalityHistory:
		return show(m.personalityHistory(ctx, v, r.id))
	case opPersonalityRevert:
		if err := m.svc.RevertPersonality(ctx, v.user, r.id, r.id2); err != nil {
			return outcome{}, err
		}
		return showWith("Личность возвращена")(m.personality(ctx, v, r.id))
	case opStyleEdit:
		return m.editSummaryStyle(ctx, v, r.id)
	case opStyleReset:
		if err := m.svc.SetSummaryStyle(ctx, v.user, r.id, ""); err != nil {
			return outcome{}, err
		}
		return showWith("Стиль сброшен")(m.personality(ctx, v, r.id))

	case opLimits:
		return show(m.limits(ctx, v, r.id))
	case opLimitUser, opLimitChat:
		c, err := m.svc.Chat(ctx, v.user, r.id)
		if err != nil {
			return outcome{}, err
		}
		in := chatInput(c)
		if r.op == opLimitUser {
			in.Settings.Limits.UserPerHour = nextStep(userLimitSteps, in.Settings.Limits.UserPerHour)
		} else {
			in.Settings.Limits.ChatPerHour = nextStep(chatLimitSteps, in.Settings.Limits.ChatPerHour)
		}
		if _, err := m.svc.UpdateChat(ctx, v.user, r.id, in); err != nil {
			return outcome{}, err
		}
		return showWith("Сохранено")(m.limits(ctx, v, r.id))
	case opStats:
		return show(m.stats(ctx, v, r.id))
	}
	return outcome{}, errBadRoute
}

// chatInput is the chat's current editable state, to change one field of.
func chatInput(c settings.ChatView) settings.ChatInput {
	return settings.ChatInput{Settings: c.Settings, SummaryModelID: c.SummaryModelID, ChatModelID: c.ChatModelID}
}

// update changes the chat with f and renders its screen.
func (m *menu) update(ctx context.Context, v view, chatID int64, f func(*settings.ChatInput)) (screen, error) {
	c, err := m.svc.Chat(ctx, v.user, chatID)
	if err != nil {
		return screen{}, err
	}
	in := chatInput(c)
	f(&in)
	if c, err = m.svc.UpdateChat(ctx, v.user, chatID, in); err != nil {
		return screen{}, err
	}
	return m.chatView(v, c), nil
}

func toggle(st *domain.ChatSettings, word string) {
	switch word {
	case toggleEnabled:
		st.Enabled = !st.Enabled
	case toggleSummary:
		st.Summary = !st.Summary
	case toggleMention:
		st.MentionAll = !st.MentionAll
	case toggleChat:
		st.Chat = !st.Chat
	}
}

func optionalID(id int64) *int64 {
	if id == 0 {
		return nil
	}
	return &id
}

// nextStep returns the step after cur, or the first one if cur is not a step.
func nextStep(steps []int, cur int) int {
	i := slices.Index(steps, cur)
	return steps[(i+1)%len(steps)]
}

func (m *menu) chatScreen(ctx context.Context, v view, chatID int64) (screen, error) {
	c, err := m.svc.Chat(ctx, v.user, chatID)
	if err != nil {
		return screen{}, err
	}
	return m.chatView(v, c), nil
}

func (m *menu) chatView(v view, c settings.ChatView) screen {
	if domain.IsPrivateChat(c.ID) {
		return m.privateChatScreen(c)
	}
	return m.groupScreen(v, c)
}

func (m *menu) groupScreen(v view, c settings.ChatView) screen {
	st := c.Settings
	var t strings.Builder
	t.WriteString("<b>Настройки «" + html.EscapeString(c.Title) + "»</b>\n\n")
	t.WriteString("Модель для пересказов: " + html.EscapeString(modelName(c.SummaryModel, "не выбрана")) + "\n")
	t.WriteString("Модель для общения: " + html.EscapeString(chatModelName(c)) + "\n")
	if c.Personality != "" {
		t.WriteString("Личность: задана\n")
	}
	if c.SummaryModel == nil {
		t.WriteString("\nПока модель для пересказов не выбрана, /summary не работает.")
	}
	if !st.Enabled {
		t.WriteString("\nБот выключен в этом чате: он не делает пересказы, не отвечает и не отзывается на @all.")
	}

	toggleButton := func(label string, on bool, word string) telego.InlineKeyboardButton {
		label = check(on) + " " + label
		if !st.Enabled {
			return disabled(label)
		}
		return button(label, route{op: opGroupToggle, id: c.ID, word: word})
	}
	power := styled(button("Включить бота", route{op: opGroupToggle, id: c.ID, word: toggleEnabled}), telego.ButtonStyleSuccess)
	if st.Enabled {
		power = button("Выключить бота", route{op: opGroupToggle, id: c.ID, word: toggleEnabled})
	}
	rows := [][]telego.InlineKeyboardButton{
		row(power),
		row(toggleButton("Пересказы", st.Summary, toggleSummary), toggleButton("@all", st.MentionAll, toggleMention)),
		row(toggleButton("Общение с ботом", st.Chat, toggleChat)),
		row(button("Модель для пересказов ›", route{op: opGroupModels, id: c.ID})),
		row(button("Модель для общения ›", route{op: opChatModels, id: c.ID})),
		row(
			button("Личность", route{op: opPersonality, id: c.ID}),
			button("Лимиты", route{op: opLimits, id: c.ID}),
			button("Статистика", route{op: opStats, id: c.ID}),
		),
	}
	if v.private {
		rows = append(rows, back("Группы", route{op: opGroups}))
	} else {
		rows = append(rows,
			row(telego.InlineKeyboardButton{Text: "Мои модели → в личку", URL: m.startLink(startParamModels)}),
			row(button("Закрыть", route{op: opClose})),
		)
	}
	return screen{text: t.String(), rows: rows}
}

// privateChatScreen is the user's own chat with the bot. It has no summaries or limits to set.
func (m *menu) privateChatScreen(c settings.ChatView) screen {
	var t strings.Builder
	t.WriteString("<b>Мой чат с ботом</b>\n\n")
	t.WriteString("Модель: " + html.EscapeString(modelName(c.ChatModel, "не выбрана")) + "\n")
	if c.Personality != "" {
		t.WriteString("Личность: задана\n")
	}
	if c.ChatModel == nil {
		t.WriteString("\nВыберите модель, чтобы бот отвечал вам здесь.")
	}
	return screen{text: t.String(), rows: [][]telego.InlineKeyboardButton{
		row(button("Модель ›", route{op: opChatModels, id: c.ID})),
		row(button("Личность", route{op: opPersonality, id: c.ID}), button("Статистика", route{op: opStats, id: c.ID})),
		back("Назад", route{op: opHome}),
	}}
}

func (m *menu) startLink(param string) string {
	return "https://t.me/" + m.botUsername + "?start=" + param
}

func modelName(o *domain.ModelOption, none string) string {
	if o == nil {
		return none
	}
	return o.DisplayName
}

func chatModelName(c settings.ChatView) string {
	if c.ChatModel == nil {
		return "как для пересказов"
	}
	return c.ChatModel.DisplayName
}

// modelPicker lists models to bind for summaries or, with forChat, for talking.
func (m *menu) modelPicker(ctx context.Context, v view, chatID int64, forChat bool) (screen, error) {
	c, err := m.svc.Chat(ctx, v.user, chatID)
	if err != nil {
		return screen{}, err
	}
	usable, err := m.svc.UsableModels(ctx, v.user)
	if err != nil {
		return screen{}, err
	}

	op, current, none := opGroupModel, c.SummaryModel, "Не выбрана"
	title := "Модель для пересказов в «" + html.EscapeString(c.Title) + "»"
	switch {
	case forChat && domain.IsPrivateChat(chatID):
		op, current = opChatModel, c.ChatModel
		title = "Модель для разговора со мной"
	case forChat:
		op, current, none = opChatModel, c.ChatModel, "Как для пересказов"
		title = "Модель для общения в «" + html.EscapeString(c.Title) + "»"
	}

	text := "<b>" + title + "</b>\n\n"
	if current != nil && !slices.ContainsFunc(usable, func(o domain.ModelOption) bool { return o.ID == current.ID }) {
		text += "Сейчас выбрана «" + html.EscapeString(current.DisplayName) + "» — личная модель " +
			html.EscapeString(cmp.Or(current.OwnerName, "другого администратора")) + ". Если выбрать другую, вернуть её сможет только владелец.\n\n"
	}
	text += "Здесь ваши модели и общие модели владельца бота. Свои можно подключить в личке с ботом, в разделе «Мои модели»."

	mark := func(on bool, label string) string {
		if on {
			return "✓ " + label
		}
		return label
	}
	rows := [][]telego.InlineKeyboardButton{
		row(button(mark(current == nil, none), route{op: op, id: chatID})),
	}
	for _, o := range capped(usable) {
		label := mark(current != nil && current.ID == o.ID, optionLabel(o, v.user))
		rows = append(rows, row(button(label, route{op: op, id: chatID, id2: o.ID})))
	}
	rows = append(rows, back("Назад", route{op: opGroup, id: chatID}))
	return screen{text: text, rows: rows}, nil
}

func optionLabel(o domain.ModelOption, viewer settings.User) string {
	if o.OwnerID == viewer.ID {
		return o.DisplayName + " · " + o.ProviderName
	}
	return o.DisplayName + " · общая, от " + cmp.Or(o.OwnerName, "владельца бота")
}

// Personality.

func (m *menu) personality(ctx context.Context, v view, chatID int64) (screen, error) {
	c, err := m.svc.Chat(ctx, v.user, chatID)
	if err != nil {
		return screen{}, err
	}
	var t strings.Builder
	t.WriteString("<b>Личность бота</b>\n\n")
	if c.Personality == "" {
		t.WriteString("Обычная: бот ведёт себя как дружелюбный помощник.\n")
	} else {
		t.WriteString("<blockquote expandable>" + html.EscapeString(c.Personality) + "</blockquote>\n")
	}
	if !domain.IsPrivateChat(chatID) {
		t.WriteString("\n<b>Стиль пересказов</b>\n")
		if c.SummaryStyle == "" {
			t.WriteString("Обычный.\n")
		} else {
			t.WriteString("<blockquote expandable>" + html.EscapeString(c.SummaryStyle) + "</blockquote>\n")
		}
		t.WriteString("\nАдминистраторы могут менять личность и просто попросив бота в чате.")
	}

	// Text is typed in the private chat, never in the group.
	var rows [][]telego.InlineKeyboardButton
	switch {
	case !v.private:
		link := m.startLink(startParamChatPrefix + strconv.FormatInt(chatID, 10))
		rows = append(rows, row(telego.InlineKeyboardButton{Text: "Изменить в личке", URL: link}))
	case domain.IsPrivateChat(chatID):
		rows = append(rows, row(button("Изменить личность", route{op: opPersonalityEdit, id: chatID})))
	default:
		rows = append(rows,
			row(button("Изменить личность", route{op: opPersonalityEdit, id: chatID})),
			row(button("Изменить стиль пересказов", route{op: opStyleEdit, id: chatID})),
		)
	}
	var resets []telego.InlineKeyboardButton
	if c.Personality != "" {
		resets = append(resets, styled(button("Сбросить личность", route{op: opPersonalityReset, id: chatID}), telego.ButtonStyleDanger))
	}
	if c.SummaryStyle != "" {
		resets = append(resets, styled(button("Сбросить стиль", route{op: opStyleReset, id: chatID}), telego.ButtonStyleDanger))
	}
	if len(resets) > 0 {
		rows = append(rows, resets)
	}
	rows = append(rows,
		row(button("История изменений", route{op: opPersonalityHistory, id: chatID})),
		back("Назад", route{op: opGroup, id: chatID}),
	)
	return screen{text: t.String(), rows: rows}, nil
}

// historyTextLimit shortens old personalities in the history list.
const historyTextLimit = 150

func (m *menu) personalityHistory(ctx context.Context, v view, chatID int64) (screen, error) {
	changes, err := m.svc.PersonalityHistory(ctx, v.user, chatID)
	if err != nil {
		return screen{}, err
	}
	var t strings.Builder
	t.WriteString("<b>История личности</b>\n\n")
	if len(changes) == 0 {
		t.WriteString("Личность ещё не меняли.")
	}
	var rows [][]telego.InlineKeyboardButton
	for i, ch := range changes {
		via := "через меню"
		if ch.Via == domain.ViaTool {
			via = "попросив бота"
		}
		text := "<i>сброс</i>"
		if ch.Text != "" {
			text = html.EscapeString(truncate(ch.Text, historyTextLimit))
		}
		fmt.Fprintf(&t, "<b>%d.</b> %s, %s %s:\n%s\n\n", i+1, ch.At.Format("02.01 15:04"),
			html.EscapeString(cmp.Or(ch.ChangedByName, "кто-то")), via, text)
		if i > 0 {
			rows = append(rows, row(button(fmt.Sprintf("Вернуть №%d", i+1), route{op: opPersonalityRevert, id: chatID, id2: ch.ID})))
		}
	}
	rows = append(rows, back("Назад", route{op: opPersonality, id: chatID}))
	return screen{text: strings.TrimSpace(t.String()), rows: rows}, nil
}

func (m *menu) editPersonality(ctx context.Context, v view, chatID int64) (outcome, error) {
	if _, err := m.svc.Chat(ctx, v.user, chatID); err != nil {
		return outcome{}, err
	}
	var text string
	return outcome{dialog: &dialog{
		steps: []step{{
			prompt: "Опишите, каким должен быть бот: характер, манера речи, роль. Например: «Ты — ворчливый пират, " +
				"отвечаешь коротко и с морскими словечками».",
			placeholder: "Ты — …",
			accept: func(v string) error {
				if err := settings.ValidatePersonality(v); err != nil {
					return err
				}
				text = v
				return nil
			},
		}},
		finish: func(ctx context.Context) (string, route, error) {
			if err := m.svc.SetPersonality(ctx, v.user, chatID, text, domain.ViaMenu); err != nil {
				return "", route{}, err
			}
			return "Личность сохранена.", route{op: opPersonality, id: chatID}, nil
		},
	}}, nil
}

func (m *menu) editSummaryStyle(ctx context.Context, v view, chatID int64) (outcome, error) {
	if _, err := m.svc.Chat(ctx, v.user, chatID); err != nil {
		return outcome{}, err
	}
	var style string
	return outcome{dialog: &dialog{
		steps: []step{{
			prompt:      "Как писать пересказы в этой группе? Например: «короче, одним абзацем» или «стихами».",
			placeholder: "Стиль пересказов",
			accept: func(v string) error {
				if err := settings.ValidateSummaryStyle(v); err != nil {
					return err
				}
				style = v
				return nil
			},
		}},
		finish: func(ctx context.Context) (string, route, error) {
			if err := m.svc.SetSummaryStyle(ctx, v.user, chatID, style); err != nil {
				return "", route{}, err
			}
			return "Стиль пересказов сохранён.", route{op: opPersonality, id: chatID}, nil
		},
	}}, nil
}

// Limits and stats.

func (m *menu) limits(ctx context.Context, v view, chatID int64) (screen, error) {
	c, err := m.svc.Chat(ctx, v.user, chatID)
	if err != nil {
		return screen{}, err
	}
	l := c.Settings.Limits
	text := "<b>Лимиты общения</b>\n\n" +
		"Сколько раз в час бот отвечает одному человеку и всему чату. Пересказы не считаются.\n\n" +
		"Нажмите на лимит, чтобы переключить его."
	return screen{text: text, rows: [][]telego.InlineKeyboardButton{
		row(button("Одному человеку: "+perHour(l.UserPerHour), route{op: opLimitUser, id: chatID})),
		row(button("Всему чату: "+perHour(l.ChatPerHour), route{op: opLimitChat, id: chatID})),
		back("Назад", route{op: opGroup, id: chatID}),
	}}, nil
}

func perHour(n int) string {
	if n == 0 {
		return "без лимита"
	}
	return strconv.Itoa(n) + " в час"
}

func (m *menu) stats(ctx context.Context, v view, chatID int64) (screen, error) {
	st, err := m.svc.Stats(ctx, v.user, chatID)
	if err != nil {
		return screen{}, err
	}
	var t strings.Builder
	t.WriteString("<b>Статистика</b>\n\n")
	for _, p := range []struct {
		label  string
		totals domain.UsageTotals
	}{{"За сутки", st.Day}, {"За 30 дней", st.Month}} {
		fmt.Fprintf(&t, "%s: %s, токенов %s (запрос %s, ответ %s)\n", p.label,
			plural(int(p.totals.Requests), "запрос", "запроса", "запросов"),
			tokens(p.totals.TokensIn+p.totals.TokensOut), tokens(p.totals.TokensIn), tokens(p.totals.TokensOut))
	}
	if len(st.TopUsers) > 0 && !domain.IsPrivateChat(chatID) {
		t.WriteString("\nЧаще всех за 30 дней:\n")
		for i, u := range st.TopUsers {
			fmt.Fprintf(&t, "%d. %s — %s, токенов %s\n", i+1, html.EscapeString(cmp.Or(u.Name, "без имени")),
				plural(int(u.Requests), "запрос", "запроса", "запросов"), tokens(u.Tokens))
		}
	}
	return screen{text: t.String(), rows: [][]telego.InlineKeyboardButton{back("Назад", route{op: opGroup, id: chatID})}}, nil
}

// tokens formats a token count compactly: 950, 12,3 тыс., 4,1 млн.
func tokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return strings.Replace(strconv.FormatFloat(float64(n)/1e6, 'f', 1, 64), ".", ",", 1) + " млн"
	case n >= 1_000:
		return strings.Replace(strconv.FormatFloat(float64(n)/1e3, 'f', 1, 64), ".", ",", 1) + " тыс."
	}
	return strconv.FormatInt(n, 10)
}
