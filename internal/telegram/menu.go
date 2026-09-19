package telegram

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mymmrac/telego"

	"github.com/feytox/kabanbot/internal/app/settings"
	"github.com/feytox/kabanbot/internal/domain"
)

// maxListButtons keeps long lists within Telegram's limit on buttons per message.
const maxListButtons = 40

// errPrivateOnly rejects provider and model screens outside a private chat,
// so API keys are never typed in a group.
var errPrivateOnly = errors.New("private chat only")

// menu renders settings screens and carries out button presses.
// Every read and change goes through settings.Service, which makes all authorization decisions.
type menu struct {
	svc         *settings.Service
	botUsername string
}

// screen is one menu message: HTML text and an inline keyboard.
type screen struct {
	text string
	rows [][]telego.InlineKeyboardButton
}

func (s screen) markup() *telego.InlineKeyboardMarkup {
	return &telego.InlineKeyboardMarkup{InlineKeyboard: s.rows}
}

// view is who is looking at a screen and where.
type view struct {
	user settings.User
	// private is true in the user's chat with the bot, false in a group.
	private bool
}

// outcome is the result of a button press.
type outcome struct {
	// screen replaces the menu message. Nil leaves it as is.
	screen *screen
	// toast is shown briefly on top of the chat.
	toast string
	// dialog asks the user for text input in the private chat.
	dialog *dialog
}

func show(s screen, err error) (outcome, error) {
	if err != nil {
		return outcome{}, err
	}
	return outcome{screen: &s}, nil
}

func showWith(toast string) func(screen, error) (outcome, error) {
	return func(s screen, err error) (outcome, error) {
		out, err := show(s, err)
		out.toast = toast
		return out, err
	}
}

// groupRoute reports whether the route may be used inside a group. Such routes all take
// the chat ID as their first ID. Text input for a chat happens in the private chat.
func groupRoute(op string) bool {
	switch op {
	case opNoop, opClose, opGroup, opGroupToggle, opGroupModels, opGroupModel, opChatModels, opChatModel,
		opPersonality, opPersonalityReset, opPersonalityHistory, opPersonalityRevert, opStyleReset,
		opLimits, opLimitUser, opLimitChat, opStats:
		return true
	}
	return false
}

// handle performs the route for the user. The route is untrusted input.
func (m *menu) handle(ctx context.Context, v view, r route) (outcome, error) {
	if !v.private && !groupRoute(r.op) {
		return outcome{}, errPrivateOnly
	}
	switch r.op {
	case opNoop, opClose:
		return outcome{}, nil
	case opHome:
		return show(m.home(v), nil)
	case opGroups:
		return show(m.groups(ctx, v))

	case opProviders:
		return show(m.providers(ctx, v))
	case opProviderNew:
		return show(m.providerKinds(), nil)
	case opProviderCreate:
		return outcome{dialog: m.newProviderDialog(v, domain.ProviderKind(r.word))}, nil
	case opProvider:
		return show(m.provider(ctx, v, r.id))
	case opProviderEdit:
		return m.editProvider(ctx, v, r.id, r.word)
	case opProviderShare:
		return showWith("Сохранено")(m.shareProvider(ctx, v, r.id))
	case opProviderDelete:
		return show(m.confirmDeleteProvider(ctx, v, r.id))
	case opProviderDrop:
		if err := m.svc.DeleteProvider(ctx, v.user, r.id); err != nil {
			return outcome{}, err
		}
		return showWith("Провайдер удалён")(m.providers(ctx, v))

	case opModelNew:
		return m.newModel(ctx, v, r.id)
	case opModel:
		return show(m.model(ctx, v, r.id, ""))
	case opModelEdit:
		return m.editModel(ctx, v, r.id, r.word)
	case opModelTest:
		return show(m.testModel(ctx, v, r.id))
	case opModelBind:
		return show(m.bindModel(ctx, v, r.id))
	case opModelBindTo:
		return m.bindModelTo(ctx, v, r.id, r.id2)
	case opModelUnbind:
		if err := m.svc.UnbindModel(ctx, v.user, r.id, r.id2); err != nil {
			return outcome{}, err
		}
		return showWith("Модель отвязана")(m.model(ctx, v, r.id, ""))
	case opModelDelete:
		return show(m.confirmDeleteModel(ctx, v, r.id))
	case opModelDrop:
		return m.dropModel(ctx, v, r.id)
	}
	return m.handleChatRoute(ctx, v, r)
}

func (m *menu) dropModel(ctx context.Context, v view, id int64) (outcome, error) {
	_, p, err := m.findModel(ctx, v.user, id)
	if err != nil {
		return outcome{}, err
	}
	if err := m.svc.DeleteModel(ctx, v.user, id); err != nil {
		return outcome{}, err
	}
	return showWith("Модель удалена")(m.provider(ctx, v, p.ID))
}

// userError returns a message about err that is safe to show, and whether err was expected.
func userError(err error) (string, bool) {
	if verr, ok := errors.AsType[*settings.ValidationError](err); ok {
		return verr.Msg, true
	}
	switch {
	case errors.Is(err, errPrivateOnly):
		return "Это меню открывается только в личке с ботом.", true
	case errors.Is(err, errBadRoute):
		return "Эта кнопка больше не работает. Откройте меню заново.", true
	case errors.Is(err, settings.ErrNotFound):
		return "Не найдено: возможно, это уже удалили.", true
	case errors.Is(err, settings.ErrForbidden):
		return "Настройки бота в группе могут менять только её администраторы.", true
	}
	return "Не получилось, попробуйте ещё раз.", false
}

// Buttons.

func button(text string, r route) telego.InlineKeyboardButton {
	return telego.InlineKeyboardButton{Text: text, CallbackData: r.String()}
}

func styled(b telego.InlineKeyboardButton, style string) telego.InlineKeyboardButton {
	b.Style = style
	return b
}

func disabled(text string) telego.InlineKeyboardButton {
	return telego.InlineKeyboardButton{Text: text, CallbackData: route{op: opNoop}.String(), Disabled: &telego.DisabledButton{}}
}

func row(buttons ...telego.InlineKeyboardButton) []telego.InlineKeyboardButton { return buttons }

func back(text string, r route) []telego.InlineKeyboardButton { return row(button("« "+text, r)) }

func check(on bool) string {
	if on {
		return "✅"
	}
	return "⬜"
}

// Main menu.

func (m *menu) home(v view) screen {
	return screen{
		text: "<b>Кабанбот</b>\n\n" +
			"Я делаю пересказы переписки в группах: ответьте командой /summary на сообщение, с которого начать.\n\n" +
			"Со мной можно поговорить: упомяните меня в группе, ответьте на моё сообщение или напишите сюда.\n\n" +
			"Здесь можно подключить свои модели и настроить группы, где вы администратор.",
		rows: [][]telego.InlineKeyboardButton{
			row(button("Группы", route{op: opGroups}), button("Мои модели", route{op: opProviders})),
			row(button("Мой чат с ботом", route{op: opGroup, id: v.user.ID})),
		},
	}
}

func (m *menu) groups(ctx context.Context, v view) (screen, error) {
	chats, err := m.svc.Chats(ctx, v.user)
	if err != nil {
		return screen{}, err
	}
	s := screen{text: "<b>Группы</b>\n\n"}
	if len(chats) == 0 {
		s.text += "Не нашёл групп, где есть бот и вы администратор. Добавьте бота в группу и напишите там что-нибудь."
	} else {
		s.text += "Группы, где есть бот и вы администратор."
	}
	for _, c := range capped(chats) {
		s.rows = append(s.rows, row(button(c.Title, route{op: opGroup, id: c.ID})))
	}
	s.rows = append(s.rows, back("Назад", route{op: opHome}))
	return s, nil
}

// Providers.

var providerKinds = []struct {
	kind    domain.ProviderKind
	label   string
	baseURL string // the default address, shown as a hint
	example string // an example model ID
}{
	{domain.ProviderOpenRouter, "OpenRouter", "https://openrouter.ai/api/v1", "google/gemini-2.5-flash"},
	{domain.ProviderGemini, "Gemini API", "https://generativelanguage.googleapis.com", "gemini-2.5-flash"},
	{domain.ProviderOpenAI, "OpenAI-совместимый", "https://api.openai.com/v1", "gpt-4.1-mini"},
}

func kindInfo(k domain.ProviderKind) (label, baseURL, example string) {
	for _, pk := range providerKinds {
		if pk.kind == k {
			return pk.label, pk.baseURL, pk.example
		}
	}
	return string(k), "", ""
}

func (m *menu) providers(ctx context.Context, v view) (screen, error) {
	list, err := m.svc.Providers(ctx, v.user)
	if err != nil {
		return screen{}, err
	}
	s := screen{text: "<b>Мои модели</b>\n\n"}
	switch {
	case len(list) == 0:
		s.text += "Подключите провайдера — OpenRouter, Gemini или любой OpenAI-совместимый API — и добавьте в него модели. " +
			"Их можно будет выбрать для пересказов в группах, где вы администратор."
	default:
		s.text += "Ваши провайдеры. Ключи хранятся зашифрованными и никому не показываются."
	}
	for _, p := range capped(list) {
		label := p.Name + " · " + plural(len(p.Models), "модель", "модели", "моделей")
		s.rows = append(s.rows, row(button(label, route{op: opProvider, id: p.ID})))
	}
	s.rows = append(s.rows,
		row(styled(button("➕ Подключить провайдера", route{op: opProviderNew}), telego.ButtonStyleSuccess)),
		back("Назад", route{op: opHome}),
	)
	return s, nil
}

func (m *menu) providerKinds() screen {
	s := screen{text: "<b>Новый провайдер</b>\n\nКакой API подключить?"}
	for _, pk := range providerKinds {
		s.rows = append(s.rows, row(button(pk.label, route{op: opProviderCreate, word: string(pk.kind)})))
	}
	s.rows = append(s.rows, back("Назад", route{op: opProviders}))
	return s
}

// findProvider returns one of the user's providers. Other users' providers are not found.
func (m *menu) findProvider(ctx context.Context, u settings.User, id int64) (settings.ProviderView, error) {
	list, err := m.svc.Providers(ctx, u)
	if err != nil {
		return settings.ProviderView{}, err
	}
	for _, p := range list {
		if p.ID == id {
			return p, nil
		}
	}
	return settings.ProviderView{}, settings.ErrNotFound
}

func (m *menu) provider(ctx context.Context, v view, id int64) (screen, error) {
	p, err := m.findProvider(ctx, v.user, id)
	if err != nil {
		return screen{}, err
	}
	label, defaultURL, _ := kindInfo(p.Kind)
	var t strings.Builder
	t.WriteString("<b>" + html.EscapeString(p.Name) + "</b>\n\n")
	t.WriteString("Тип: " + label + "\n")
	if p.BaseURL == "" {
		t.WriteString("Адрес API: стандартный, " + html.EscapeString(defaultURL) + "\n")
	} else {
		t.WriteString("Адрес API: " + html.EscapeString(p.BaseURL) + "\n")
	}
	t.WriteString("Ключ: " + keyHint(p.KeyHint) + "\n")
	owner := m.svc.IsOwner(v.user)
	if owner {
		t.WriteString("Общий для всех групп: " + yesNo(p.Shared) + "\n")
	}
	if len(p.Models) == 0 {
		t.WriteString("\nМоделей пока нет. Добавьте хотя бы одну.")
	}

	var rows [][]telego.InlineKeyboardButton
	for _, mv := range capped(p.Models) {
		rows = append(rows, row(button(mv.DisplayName, route{op: opModel, id: mv.ID})))
	}
	rows = append(rows,
		row(styled(button("➕ Добавить модель", route{op: opModelNew, id: id}), telego.ButtonStyleSuccess)),
		row(
			button("Название", route{op: opProviderEdit, id: id, word: fieldName}),
			button("Адрес API", route{op: opProviderEdit, id: id, word: fieldURL}),
			button("Ключ", route{op: opProviderEdit, id: id, word: fieldKey}),
		),
	)
	if owner {
		rows = append(rows, row(button(check(p.Shared)+" Общий для всех групп", route{op: opProviderShare, id: id})))
	}
	rows = append(rows,
		row(styled(button("Удалить провайдера", route{op: opProviderDelete, id: id}), telego.ButtonStyleDanger)),
		back("Мои модели", route{op: opProviders}),
	)
	return screen{text: t.String(), rows: rows}, nil
}

func keyHint(hint string) string {
	if hint == "" {
		return "сохранён"
	}
	return "••••" + html.EscapeString(hint)
}

func keySaved(key string) string {
	if hint := domain.KeyHint(key); hint != "" {
		return "Ключ сохранён ••••" + hint + "."
	}
	return "Ключ сохранён."
}

func yesNo(b bool) string {
	if b {
		return "да"
	}
	return "нет"
}

func (m *menu) shareProvider(ctx context.Context, v view, id int64) (screen, error) {
	p, err := m.findProvider(ctx, v.user, id)
	if err != nil {
		return screen{}, err
	}
	_, err = m.svc.UpdateProvider(ctx, v.user, id, settings.ProviderInput{
		Kind: p.Kind, Name: p.Name, BaseURL: p.BaseURL, Shared: !p.Shared,
	})
	if err != nil {
		return screen{}, err
	}
	return m.provider(ctx, v, id)
}

func (m *menu) confirmDeleteProvider(ctx context.Context, v view, id int64) (screen, error) {
	p, err := m.findProvider(ctx, v.user, id)
	if err != nil {
		return screen{}, err
	}
	text := "Удалить провайдера «" + html.EscapeString(p.Name) + "»"
	if n := len(p.Models); n > 0 {
		text += " вместе с его моделями (" + strconv.Itoa(n) + ")? Группы, где они выбраны, останутся без модели."
	} else {
		text += "?"
	}
	return confirmScreen(text, route{op: opProviderDrop, id: id}, route{op: opProvider, id: id}), nil
}

func confirmScreen(text string, yes, no route) screen {
	return screen{text: text, rows: [][]telego.InlineKeyboardButton{
		row(styled(button("Да, удалить", yes), telego.ButtonStyleDanger), button("Отмена", no)),
	}}
}

// Provider dialogs.

func nameStep(dst *string) step {
	return step{
		prompt:      "Как назвать провайдера? Например, «Мой OpenRouter».",
		placeholder: "Название",
		accept: func(v string) error {
			if err := settings.ValidateProviderName(v); err != nil {
				return err
			}
			*dst = v
			return nil
		},
	}
}

func urlStep(kind domain.ProviderKind, dst *string) step {
	_, defaultURL, _ := kindInfo(kind)
	prompt := "Отправьте адрес API или «-», чтобы использовать стандартный: " + defaultURL + "."
	if kind == domain.ProviderOpenAI {
		prompt = "Отправьте адрес API OpenAI-совместимого сервиса, например https://api.example.com/v1. " +
			"Для самого OpenAI отправьте «-»."
	}
	return step{
		prompt:      prompt,
		placeholder: "https://…",
		optional:    true,
		accept: func(v string) error {
			if err := settings.ValidateBaseURL(v); err != nil {
				return err
			}
			*dst = v
			return nil
		},
	}
}

func keyStep(prompt string, dst *string) step {
	return step{
		prompt:      prompt + " Я сразу удалю сообщение с ним, а ключ сохраню в зашифрованном виде.",
		placeholder: "API-ключ",
		secret:      true,
		accept: func(v string) error {
			if err := settings.ValidateAPIKey(v); err != nil {
				return err
			}
			*dst = v
			return nil
		},
	}
}

func (m *menu) newProviderDialog(v view, kind domain.ProviderKind) *dialog {
	in := settings.ProviderInput{Kind: kind}
	return &dialog{
		steps: []step{
			nameStep(&in.Name),
			urlStep(kind, &in.BaseURL),
			keyStep("Отправьте API-ключ.", &in.APIKey),
		},
		finish: func(ctx context.Context) (string, route, error) {
			p, err := m.svc.CreateProvider(ctx, v.user, in)
			if err != nil {
				return "", route{}, err
			}
			notice := keySaved(in.APIKey) + " Провайдер «" + p.Name + "» подключён, теперь добавьте в него модель."
			return notice, route{op: opModelNew, id: p.ID}, nil
		},
	}
}

func (m *menu) editProvider(ctx context.Context, v view, id int64, field string) (outcome, error) {
	p, err := m.findProvider(ctx, v.user, id)
	if err != nil {
		return outcome{}, err
	}
	in := settings.ProviderInput{Kind: p.Kind, Name: p.Name, BaseURL: p.BaseURL, Shared: p.Shared}
	var steps []step
	switch field {
	case fieldName:
		steps = []step{nameStep(&in.Name)}
	case fieldURL:
		steps = []step{
			urlStep(p.Kind, &in.BaseURL),
			keyStep("При смене адреса нужно заново ввести ключ. Отправьте API-ключ.", &in.APIKey),
		}
	case fieldKey:
		steps = []step{keyStep("Отправьте новый API-ключ.", &in.APIKey)}
	default:
		return outcome{}, errBadRoute
	}
	d := &dialog{
		steps: steps,
		finish: func(ctx context.Context) (string, route, error) {
			// Only the fields asked for change; the rest may have been edited meanwhile.
			cur, err := m.findProvider(ctx, v.user, id)
			if err != nil {
				return "", route{}, err
			}
			upd := settings.ProviderInput{Kind: cur.Kind, Name: cur.Name, BaseURL: cur.BaseURL, Shared: cur.Shared, APIKey: in.APIKey}
			switch field {
			case fieldName:
				upd.Name = in.Name
			case fieldURL:
				upd.BaseURL = in.BaseURL
			}
			if _, err := m.svc.UpdateProvider(ctx, v.user, id, upd); err != nil {
				return "", route{}, err
			}
			notice := "Сохранено."
			if in.APIKey != "" {
				notice = keySaved(in.APIKey)
			}
			return notice, route{op: opProvider, id: id}, nil
		},
	}
	return outcome{dialog: d}, nil
}

// Models.

// findModel returns one of the user's models and its provider. Other users' models are not found.
func (m *menu) findModel(ctx context.Context, u settings.User, id int64) (settings.ModelView, settings.ProviderView, error) {
	list, err := m.svc.Providers(ctx, u)
	if err != nil {
		return settings.ModelView{}, settings.ProviderView{}, err
	}
	for _, p := range list {
		for _, mv := range p.Models {
			if mv.ID == id {
				return mv, p, nil
			}
		}
	}
	return settings.ModelView{}, settings.ProviderView{}, settings.ErrNotFound
}

// model renders a model screen. note is added to the text, e.g. a test result.
func (m *menu) model(ctx context.Context, v view, id int64, note string) (screen, error) {
	mv, p, err := m.findModel(ctx, v.user, id)
	if err != nil {
		return screen{}, err
	}
	var t strings.Builder
	t.WriteString("<b>" + html.EscapeString(mv.DisplayName) + "</b>\n\n")
	t.WriteString("ID модели: <code>" + html.EscapeString(mv.Name) + "</code>\n")
	t.WriteString("Провайдер: " + html.EscapeString(p.Name) + "\n")
	if temp := mv.Params.Temperature; temp != nil {
		t.WriteString("Temperature: " + strconv.FormatFloat(*temp, 'f', -1, 64) + "\n")
	} else {
		t.WriteString("Temperature: по умолчанию\n")
	}
	if mv.Params.MaxTokens > 0 {
		t.WriteString("Лимит токенов ответа: " + strconv.FormatInt(mv.Params.MaxTokens, 10) + "\n")
	} else {
		t.WriteString("Лимит токенов ответа: нет\n")
	}
	if len(mv.Chats) == 0 {
		t.WriteString("Группы: пока не подключена")
	} else {
		titles := make([]string, len(mv.Chats))
		for i, c := range mv.Chats {
			titles[i] = "«" + html.EscapeString(c.Title) + "»"
		}
		t.WriteString("Группы: " + strings.Join(titles, ", "))
	}
	if note != "" {
		t.WriteString("\n\n" + note)
	}

	rows := [][]telego.InlineKeyboardButton{
		row(
			styled(button("Проверить", route{op: opModelTest, id: id}), telego.ButtonStylePrimary),
			button("Подключить к группе", route{op: opModelBind, id: id}),
		),
		row(
			button("Название", route{op: opModelEdit, id: id, word: fieldDisplayName}),
			button("ID модели", route{op: opModelEdit, id: id, word: fieldName}),
		),
		row(
			button("Temperature", route{op: opModelEdit, id: id, word: fieldTemperature}),
			button("Лимит токенов", route{op: opModelEdit, id: id, word: fieldMaxTokens}),
		),
	}
	for _, c := range capped(mv.Chats) {
		rows = append(rows, row(button("Отвязать от «"+c.Title+"»", route{op: opModelUnbind, id: id, id2: c.ID})))
	}
	rows = append(rows,
		row(styled(button("Удалить модель", route{op: opModelDelete, id: id}), telego.ButtonStyleDanger)),
		back(p.Name, route{op: opProvider, id: p.ID}),
	)
	return screen{text: t.String(), rows: rows}, nil
}

// testReplyLimit keeps a model's test reply short enough for the screen.
const testReplyLimit = 1000

func (m *menu) testModel(ctx context.Context, v view, id int64) (screen, error) {
	reply, took, err := m.svc.TestModel(ctx, v.user, id)
	switch {
	case errors.Is(err, settings.ErrNotFound):
		return screen{}, err
	case err != nil:
		// The model and its provider belong to the user, so the provider's error is theirs to see.
		d := describeLLMError(err)
		if d == "" {
			d = truncate(err.Error(), testReplyLimit)
		}
		return m.model(ctx, v, id, "❌ Модель не ответила.\n"+html.EscapeString(d))
	}
	note := fmt.Sprintf("✅ Модель ответила за %.1f с: %s", took.Round(100*time.Millisecond).Seconds(),
		html.EscapeString(truncate(reply, testReplyLimit)))
	return m.model(ctx, v, id, note)
}

func (m *menu) newModel(ctx context.Context, v view, providerID int64) (outcome, error) {
	p, err := m.findProvider(ctx, v.user, providerID)
	if err != nil {
		return outcome{}, err
	}
	_, _, example := kindInfo(p.Kind)
	var in settings.ModelInput
	d := &dialog{
		steps: []step{
			modelNameStep(example, &in.Name),
			displayNameStep(&in.DisplayName),
		},
		finish: func(ctx context.Context) (string, route, error) {
			mdl, err := m.svc.CreateModel(ctx, v.user, providerID, in)
			if err != nil {
				return "", route{}, err
			}
			return "Модель добавлена. Проверьте, что она отвечает, и подключите её к группе.", route{op: opModel, id: mdl.ID}, nil
		},
	}
	return outcome{dialog: d}, nil
}

func modelNameStep(example string, dst *string) step {
	return step{
		prompt:      "Отправьте ID модели, как его называет провайдер, например " + example + ".",
		placeholder: example,
		accept: func(v string) error {
			if err := settings.ValidateModelName(v); err != nil {
				return err
			}
			*dst = v
			return nil
		},
	}
}

func displayNameStep(dst *string) step {
	return step{
		prompt:      "Как показывать модель в меню? Отправьте «-», чтобы показывать её ID.",
		placeholder: "Название",
		optional:    true,
		accept: func(v string) error {
			if err := settings.ValidateModelDisplayName(v); err != nil {
				return err
			}
			*dst = v
			return nil
		},
	}
}

func (m *menu) editModel(ctx context.Context, v view, id int64, field string) (outcome, error) {
	_, p, err := m.findModel(ctx, v.user, id)
	if err != nil {
		return outcome{}, err
	}
	var (
		name, display string
		temp          *float64
		maxTokens     int64
		st            step
	)
	switch field {
	case fieldName:
		_, _, example := kindInfo(p.Kind)
		st = modelNameStep(example, &name)
	case fieldDisplayName:
		st = displayNameStep(&display)
	case fieldTemperature:
		st = step{
			prompt:      "Отправьте temperature от 0 до 2, например 0.7, или «-», чтобы использовать значение провайдера.",
			placeholder: "0.7",
			optional:    true,
			accept: func(v string) error {
				if v == "" {
					temp = nil
					return nil
				}
				f, err := strconv.ParseFloat(strings.ReplaceAll(v, ",", "."), 64)
				if err != nil {
					return &settings.ValidationError{Msg: "Нужно число, например 0.7"}
				}
				temp = &f
				return settings.ValidateTemperature(temp)
			},
		}
	case fieldMaxTokens:
		st = step{
			prompt:      "Отправьте лимит токенов в ответе модели или «-», чтобы убрать лимит.",
			placeholder: "4000",
			optional:    true,
			accept: func(v string) error {
				if v == "" {
					maxTokens = 0
					return nil
				}
				n, err := strconv.ParseInt(strings.ReplaceAll(v, " ", ""), 10, 64)
				if err != nil {
					return &settings.ValidationError{Msg: "Нужно целое число, например 4000"}
				}
				maxTokens = n
				return settings.ValidateMaxTokens(n)
			},
		}
	default:
		return outcome{}, errBadRoute
	}
	d := &dialog{
		steps: []step{st},
		finish: func(ctx context.Context) (string, route, error) {
			// Only the field asked for changes; the rest may have been edited meanwhile.
			cur, _, err := m.findModel(ctx, v.user, id)
			if err != nil {
				return "", route{}, err
			}
			in := settings.ModelInput{Name: cur.Name, DisplayName: cur.DisplayName, Params: cur.Params}
			switch field {
			case fieldName:
				// A display name that just mirrored the old ID follows the new one.
				if in.DisplayName == in.Name {
					in.DisplayName = ""
				}
				in.Name = name
			case fieldDisplayName:
				in.DisplayName = display
			case fieldTemperature:
				in.Params.Temperature = temp
			case fieldMaxTokens:
				in.Params.MaxTokens = maxTokens
			}
			if _, err := m.svc.UpdateModel(ctx, v.user, id, in); err != nil {
				return "", route{}, err
			}
			return "Сохранено.", route{op: opModel, id: id}, nil
		},
	}
	return outcome{dialog: d}, nil
}

func (m *menu) bindModel(ctx context.Context, v view, id int64) (screen, error) {
	mv, _, err := m.findModel(ctx, v.user, id)
	if err != nil {
		return screen{}, err
	}
	chats, err := m.svc.Chats(ctx, v.user)
	if err != nil {
		return screen{}, err
	}
	s := screen{text: "<b>Подключить «" + html.EscapeString(mv.DisplayName) + "»</b>\n\n"}
	if len(chats) == 0 {
		s.text += "Не нашёл групп, где есть бот и вы администратор."
	} else {
		s.text += "В какой группе делать пересказы этой моделью?"
	}
	for _, c := range capped(chats) {
		label := c.Title
		if c.SummaryModelID != nil && *c.SummaryModelID == id {
			label = "✓ " + label
		}
		s.rows = append(s.rows, row(button(label, route{op: opModelBindTo, id: id, id2: c.ID})))
	}
	s.rows = append(s.rows, back("Назад", route{op: opModel, id: id}))
	return s, nil
}

func (m *menu) bindModelTo(ctx context.Context, v view, id, chatID int64) (outcome, error) {
	c, err := m.svc.Chat(ctx, v.user, chatID)
	if err != nil {
		return outcome{}, err
	}
	in := chatInput(c)
	in.SummaryModelID = &id
	if _, err := m.svc.UpdateChat(ctx, v.user, chatID, in); err != nil {
		return outcome{}, err
	}
	return showWith("Модель подключена к «" + c.Title + "»")(m.model(ctx, v, id, ""))
}

func (m *menu) confirmDeleteModel(ctx context.Context, v view, id int64) (screen, error) {
	mv, _, err := m.findModel(ctx, v.user, id)
	if err != nil {
		return screen{}, err
	}
	text := "Удалить модель «" + html.EscapeString(mv.DisplayName) + "»?"
	if len(mv.Chats) > 0 {
		text += " Группы, где она выбрана, останутся без модели."
	}
	return confirmScreen(text, route{op: opModelDrop, id: id}, route{op: opModel, id: id}), nil
}

// Helpers.

func capped[T any](s []T) []T { return s[:min(len(s), maxListButtons)] }

func truncate(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit]) + "…"
}

// plural formats n with the Russian noun form for 1, 2–4 and 5+.
func plural(n int, one, few, many string) string {
	form := many
	switch n10, n100 := n%10, n%100; {
	case n10 == 1 && n100 != 11:
		form = one
	case n10 >= 2 && n10 <= 4 && (n100 < 12 || n100 > 14):
		form = few
	}
	return strconv.Itoa(n) + " " + form
}
