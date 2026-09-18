package telegram

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/feytox/kabanbot/internal/domain"
)

// adminCacheTTL bounds how long a revoked admin keeps access to chat settings.
const adminCacheTTL = 5 * time.Minute

type adminKey struct{ chatID, userID int64 }

type adminEntry struct {
	isAdmin bool
	expires time.Time
}

// adminCache remembers getChatMember results, since the Mini App checks them on every request.
type adminCache struct {
	mu      sync.Mutex
	entries map[adminKey]adminEntry
}

func (c *adminCache) get(k adminKey, now time.Time) (bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[k]
	if !ok || now.After(e.expires) {
		delete(c.entries, k)
		return false, false
	}
	return e.isAdmin, true
}

func (c *adminCache) put(k adminKey, isAdmin bool, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[adminKey]adminEntry)
	}
	c.entries[k] = adminEntry{isAdmin: isAdmin, expires: now.Add(adminCacheTTL)}
}

// IsAdmin reports whether the user is an administrator or the creator of the chat.
func (c *Client) IsAdmin(ctx context.Context, chatID, userID int64) (bool, error) {
	k := adminKey{chatID, userID}
	if v, ok := c.admins.get(k, time.Now()); ok {
		return v, nil
	}
	m, err := c.api.GetChatMember(ctx, &telego.GetChatMemberParams{ChatID: tu.ID(chatID), UserID: userID})
	if err != nil {
		return false, fmt.Errorf("get chat member: %w", err)
	}
	status := m.MemberStatus()
	isAdmin := status == telego.MemberStatusCreator || status == telego.MemberStatusAdministrator
	c.admins.put(k, isAdmin, time.Now())
	return isAdmin, nil
}

// Admins lists the chat's human administrators. It implements mention.Admins.
func (c *Client) Admins(ctx context.Context, chatID int64) ([]domain.User, error) {
	members, err := c.api.GetChatAdministrators(ctx, &telego.GetChatAdministratorsParams{ChatID: tu.ID(chatID)})
	if err != nil {
		return nil, fmt.Errorf("get chat administrators: %w", err)
	}
	var out []domain.User
	for _, m := range members {
		if u := m.MemberUser(); !u.IsBot {
			out = append(out, domain.User{ID: u.ID, Name: fullName(u)})
		}
	}
	return out, nil
}
