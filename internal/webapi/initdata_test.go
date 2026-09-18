package webapi

import (
	"encoding/hex"
	"errors"
	"net/url"
	"strconv"
	"testing"
	"time"

	tu "github.com/mymmrac/telego/telegoutil"
)

const testToken = "123456:TEST-token"

func signedInitData(token string, authDate time.Time, user string) string {
	v := url.Values{}
	v.Set("query_id", "AAH")
	v.Set("user", user)
	v.Set("auth_date", strconv.FormatInt(authDate.Unix(), 10))
	v.Set("start_param", "chat_-100")
	v.Set("hash", hex.EncodeToString(sign(v, token)))
	return v.Encode()
}

func TestParseInitData(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	raw := signedInitData(testToken, now.Add(-time.Hour), `{"id":42,"first_name":"Ann","username":"ann"}`)

	// Cross-check the signing algorithm against an independent implementation.
	if _, err := tu.ValidateWebAppData(testToken, raw); err != nil {
		t.Fatalf("telego rejects our signature: %v", err)
	}

	got, err := parseInitData(raw, testToken, 24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.User.ID != 42 || got.User.Username != "ann" || got.StartParam != "chat_-100" {
		t.Errorf("got %+v", got)
	}
}

func TestParseInitDataRejects(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	user := `{"id":42}`
	valid := signedInitData(testToken, now, user)
	tampered, _ := url.ParseQuery(valid)
	tampered.Set("user", `{"id":1}`)

	for name, raw := range map[string]string{
		"wrong token": signedInitData("999:other", now, user),
		"expired":     signedInitData(testToken, now.Add(-25*time.Hour), user),
		"tampered":    tampered.Encode(),
		"no hash":     "user=%7B%22id%22%3A42%7D&auth_date=1",
		"no user":     signedInitData(testToken, now, ``),
		"garbage":     "%%%",
	} {
		if _, err := parseInitData(raw, testToken, 24*time.Hour, now); !errors.Is(err, errInitData) {
			t.Errorf("%s: err = %v, want errInitData", name, err)
		}
	}
}
