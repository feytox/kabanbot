package webapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// initData is the validated launch data of a Mini App.
type initData struct {
	User       tgUser
	StartParam string
	AuthDate   time.Time
}

type tgUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

var errInitData = errors.New("invalid init data")

// parseInitData validates Telegram.WebApp.initData as described in
// https://core.telegram.org/bots/webapps#validating-data-received-via-the-mini-app
func parseInitData(raw, botToken string, maxAge time.Duration, now time.Time) (initData, error) {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return initData{}, fmt.Errorf("%w: %w", errInitData, err)
	}
	hash, err := hex.DecodeString(values.Get("hash"))
	if err != nil || len(hash) == 0 {
		return initData{}, fmt.Errorf("%w: bad hash", errInitData)
	}
	if !hmac.Equal(hash, sign(values, botToken)) {
		return initData{}, fmt.Errorf("%w: hash mismatch", errInitData)
	}

	unix, err := strconv.ParseInt(values.Get("auth_date"), 10, 64)
	if err != nil {
		return initData{}, fmt.Errorf("%w: bad auth_date", errInitData)
	}
	authDate := time.Unix(unix, 0)
	if now.Sub(authDate) > maxAge {
		return initData{}, fmt.Errorf("%w: expired", errInitData)
	}

	var user tgUser
	if err := json.Unmarshal([]byte(values.Get("user")), &user); err != nil || user.ID == 0 {
		return initData{}, fmt.Errorf("%w: bad user", errInitData)
	}
	return initData{User: user, StartParam: values.Get("start_param"), AuthDate: authDate}, nil
}

// sign computes the init data hash over every field except "hash".
func sign(values url.Values, botToken string) []byte {
	keys := make([]string, 0, len(values))
	for k := range values {
		if k != "hash" {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	lines := make([]string, len(keys))
	for i, k := range keys {
		lines[i] = k + "=" + values.Get(k)
	}

	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(botToken))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(strings.Join(lines, "\n")))
	return mac.Sum(nil)
}
