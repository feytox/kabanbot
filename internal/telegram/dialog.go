package telegram

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/feytox/kabanbot/internal/app/settings"
)

// dialogTTL is how long the bot waits for the next answer before forgetting a dialog.
const dialogTTL = 10 * time.Minute

// skipInput is what a user sends to leave an optional field empty.
const skipInput = "-"

// step asks the user for one value in a private chat.
type step struct {
	prompt string
	// placeholder is shown in the empty input field.
	placeholder string
	// optional steps accept skipInput as an empty value.
	optional bool
	// secret input is deleted from the chat as soon as it arrives.
	secret bool
	// accept checks the value and stores it. A settings.ValidationError is shown to the user,
	// who is asked again.
	accept func(v string) error
}

// dialog collects values step by step, then saves them with finish.
type dialog struct {
	steps []step
	// finish saves the collected values. It returns a notice for the user and the screen to show.
	// A settings.ValidationError keeps the dialog at its last step.
	finish func(ctx context.Context) (notice string, next route, err error)

	i       int
	expires time.Time
}

// dialogReply is what the bot should do after an answer.
type dialogReply struct {
	// deleteInput asks to delete the user's message.
	deleteInput bool
	// notice is sent first, if not empty.
	notice string
	// prompt asks for the next value. Empty means the dialog is over.
	prompt *step
	// next is the screen to show when the dialog is over. Nil means none.
	next *route
}

// dialogs keeps each user's current dialog in memory. Losing them on restart is fine.
type dialogs struct {
	mu     sync.Mutex
	active map[int64]*dialog
	// busy marks users whose answer is being processed, so a concurrent answer is ignored.
	busy map[int64]bool
}

// start replaces the user's dialog with d and returns its first question.
func (ds *dialogs) start(userID int64, d *dialog) step {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	if ds.active == nil {
		ds.active, ds.busy = make(map[int64]*dialog), make(map[int64]bool)
	}
	ds.sweep()
	d.i, d.expires = 0, time.Now().Add(dialogTTL)
	ds.active[userID] = d
	return d.steps[0]
}

// has reports whether the user is in a dialog, so their next message is an answer.
func (ds *dialogs) has(userID int64) bool {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	d, ok := ds.active[userID]
	return ok && time.Now().Before(d.expires)
}

// cancel drops the user's dialog and reports whether there was one.
func (ds *dialogs) cancel(userID int64) bool {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	d, ok := ds.active[userID]
	delete(ds.active, userID)
	return ok && time.Now().Before(d.expires)
}

// answer feeds the user's message to their dialog. It reports false when there is no dialog.
func (ds *dialogs) answer(ctx context.Context, userID int64, text string) (dialogReply, bool, error) {
	d, ok := ds.take(userID)
	if !ok {
		return dialogReply{}, false, nil
	}
	reply, done, err := d.answer(ctx, strings.TrimSpace(text))
	ds.release(userID, d, done || err != nil)
	return reply, true, err
}

func (ds *dialogs) take(userID int64) (*dialog, bool) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	d, ok := ds.active[userID]
	if !ok || ds.busy[userID] {
		return nil, false
	}
	if time.Now().After(d.expires) {
		delete(ds.active, userID)
		return nil, false
	}
	ds.busy[userID] = true
	return d, true
}

func (ds *dialogs) release(userID int64, d *dialog, done bool) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	delete(ds.busy, userID)
	// The user may have started another dialog meanwhile; keep that one.
	if ds.active[userID] != d {
		return
	}
	if done {
		delete(ds.active, userID)
		return
	}
	d.expires = time.Now().Add(dialogTTL)
}

// sweep forgets expired dialogs. The caller holds ds.mu.
func (ds *dialogs) sweep() {
	now := time.Now()
	for id, d := range ds.active {
		if now.After(d.expires) && !ds.busy[id] {
			delete(ds.active, id)
		}
	}
}

// answer applies one answer. done reports that the dialog is over.
func (d *dialog) answer(ctx context.Context, text string) (reply dialogReply, done bool, err error) {
	cur := d.steps[d.i]
	reply.deleteInput = cur.secret
	if text == "" {
		reply.notice, reply.prompt = "Нужен текстовый ответ.", &d.steps[d.i]
		return reply, false, nil
	}
	if cur.optional && text == skipInput {
		text = ""
	}
	if err := cur.accept(text); err != nil {
		return d.retry(reply, err)
	}
	if d.i+1 < len(d.steps) {
		d.i++
		reply.prompt = &d.steps[d.i]
		return reply, false, nil
	}

	notice, next, err := d.finish(ctx)
	if err != nil {
		return d.retry(reply, err)
	}
	reply.notice, reply.next = notice, &next
	return reply, true, nil
}

// retry asks the current step again if err is the user's mistake, and ends the dialog otherwise.
func (d *dialog) retry(reply dialogReply, err error) (dialogReply, bool, error) {
	verr, ok := errors.AsType[*settings.ValidationError](err)
	if !ok {
		return reply, true, err
	}
	reply.notice, reply.prompt = verr.Msg, &d.steps[d.i]
	return reply, false, nil
}
