package telegram

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/feytox/kabanbot/internal/domain"
)

// callbackDataLimit is the maximum size of an inline button's callback data.
const callbackDataLimit = 64

// Menu routes: what a callback button does. Callback data is "op[:id[:id2]][:word]".
const (
	opNoop  = "noop"  // a disabled button
	opClose = "close" // delete the menu message
	opStop  = "stop"  // stop:<answer> stop an answer being written in a private chat

	opHome   = "home" // private main menu
	opGroups = "gl"   // groups where the user is an admin

	opGroup       = "g"  // g:<chat> group settings
	opGroupToggle = "gt" // gt:<chat>:<on|sum|all>
	opGroupModels = "gp" // gp:<chat> summary model picker
	opGroupModel  = "gm" // gm:<chat>:<model>; model 0 unbinds the model

	opChatModels         = "gcp" // gcp:<chat> chat model picker
	opChatModel          = "gcm" // gcm:<chat>:<model>; model 0 falls back to the summary model
	opPersonality        = "pp"  // pp:<chat>
	opPersonalityEdit    = "ppe" // ppe:<chat> start the personality dialog
	opPersonalityReset   = "ppr" // ppr:<chat>
	opPersonalityHistory = "pph" // pph:<chat>
	opPersonalityRevert  = "ppv" // ppv:<chat>:<change>
	opStyleEdit          = "pse" // pse:<chat> start the summary style dialog
	opStyleReset         = "psr" // psr:<chat>
	opLimits             = "lim" // lim:<chat>
	opLimitUser          = "lu"  // lu:<chat> cycle the per-user limit
	opLimitChat          = "lc"  // lc:<chat> cycle the per-chat limit
	opStats              = "st"  // st:<chat>
	opTrigger            = "tr"  // tr:<chat> the name the bot answers to
	opTriggerName        = "trn" // trn:<chat> start the name dialog
	opTriggerRegex       = "trr" // trr:<chat> start the pattern dialog
	opTriggerOff         = "tro" // tro:<chat>

	opProviders      = "pl"  // the user's providers
	opProviderNew    = "pn"  // choose the kind of a new provider
	opProviderCreate = "pk"  // pk:<kind> start the new provider dialog
	opProvider       = "p"   // p:<provider>
	opProviderEdit   = "pe"  // pe:<provider>:<name|url|key>
	opProviderShare  = "ps"  // ps:<provider> toggle shared
	opProviderDelete = "pd"  // pd:<provider> ask to confirm
	opProviderDrop   = "pdy" // pdy:<provider> delete for real

	opModelNew    = "mn"  // mn:<provider> start the new model dialog
	opModel       = "m"   // m:<model>
	opModelEdit   = "me"  // me:<model>:<name|disp|temp|max>
	opModelTest   = "mt"  // mt:<model>
	opModelBind   = "mb"  // mb:<model> choose a group
	opModelBindTo = "mbg" // mbg:<model>:<chat>
	opModelUnbind = "mu"  // mu:<model>:<chat>
	opModelDelete = "md"  // md:<model> ask to confirm
	opModelDrop   = "mdy" // mdy:<model> delete for real
)

// Words allowed in routes.
const (
	toggleEnabled = "on"
	toggleSummary = "sum"
	toggleMention = "all"
	toggleChat    = "talk"

	fieldName        = "name"
	fieldURL         = "url"
	fieldKey         = "key"
	fieldDisplayName = "disp"
	fieldTemperature = "temp"
	fieldMaxTokens   = "max"
)

type routeSpec struct {
	ids   int
	words []string // nil means the route takes no word
}

var routeSpecs = map[string]routeSpec{
	opNoop:  {},
	opClose: {},
	opStop:  {ids: 1},

	opHome:   {},
	opGroups: {},

	opGroup:       {ids: 1},
	opGroupToggle: {ids: 1, words: []string{toggleEnabled, toggleSummary, toggleMention, toggleChat}},
	opGroupModels: {ids: 1},
	opGroupModel:  {ids: 2},

	opChatModels:         {ids: 1},
	opChatModel:          {ids: 2},
	opPersonality:        {ids: 1},
	opPersonalityEdit:    {ids: 1},
	opPersonalityReset:   {ids: 1},
	opPersonalityHistory: {ids: 1},
	opPersonalityRevert:  {ids: 2},
	opStyleEdit:          {ids: 1},
	opStyleReset:         {ids: 1},
	opLimits:             {ids: 1},
	opLimitUser:          {ids: 1},
	opLimitChat:          {ids: 1},
	opStats:              {ids: 1},
	opTrigger:            {ids: 1},
	opTriggerName:        {ids: 1},
	opTriggerRegex:       {ids: 1},
	opTriggerOff:         {ids: 1},

	opProviders:   {},
	opProviderNew: {},
	opProviderCreate: {words: []string{
		string(domain.ProviderOpenAI), string(domain.ProviderOpenRouter), string(domain.ProviderGemini),
	}},
	opProvider:       {ids: 1},
	opProviderEdit:   {ids: 1, words: []string{fieldName, fieldURL, fieldKey}},
	opProviderShare:  {ids: 1},
	opProviderDelete: {ids: 1},
	opProviderDrop:   {ids: 1},

	opModelNew:    {ids: 1},
	opModel:       {ids: 1},
	opModelEdit:   {ids: 1, words: []string{fieldName, fieldDisplayName, fieldTemperature, fieldMaxTokens}},
	opModelTest:   {ids: 1},
	opModelBind:   {ids: 1},
	opModelBindTo: {ids: 2},
	opModelUnbind: {ids: 2},
	opModelDelete: {ids: 1},
	opModelDrop:   {ids: 1},
}

// route is a parsed callback. It comes from the client, so it is untrusted:
// parsing only checks its shape, never whether the user may do it.
type route struct {
	op   string
	id   int64
	id2  int64
	word string
}

var errBadRoute = errors.New("bad callback data")

// String encodes the route as callback data.
func (r route) String() string {
	spec := routeSpecs[r.op]
	parts := []string{r.op}
	for _, id := range []int64{r.id, r.id2}[:spec.ids] {
		parts = append(parts, strconv.FormatInt(id, 10))
	}
	if spec.words != nil {
		parts = append(parts, r.word)
	}
	return strings.Join(parts, ":")
}

// parseRoute decodes callback data. Anything malformed is rejected with errBadRoute.
func parseRoute(data string) (route, error) {
	if len(data) > callbackDataLimit {
		return route{}, errBadRoute
	}
	parts := strings.Split(data, ":")
	spec, ok := routeSpecs[parts[0]]
	if !ok {
		return route{}, errBadRoute
	}
	want := 1 + spec.ids
	if spec.words != nil {
		want++
	}
	if len(parts) != want {
		return route{}, errBadRoute
	}

	r := route{op: parts[0]}
	for i, dst := range []*int64{&r.id, &r.id2}[:spec.ids] {
		id, err := strconv.ParseInt(parts[1+i], 10, 64)
		if err != nil {
			return route{}, errBadRoute
		}
		*dst = id
	}
	if spec.words != nil {
		r.word = parts[len(parts)-1]
		if !slices.Contains(spec.words, r.word) {
			return route{}, errBadRoute
		}
	}
	return r, nil
}
