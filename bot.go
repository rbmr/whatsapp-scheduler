package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// A job that is found overdue by more than this (the worker was down) is
// reported as missed instead of being sent late.
const maxLate = 10 * time.Minute

const help = `🤖 *Commands*
- ` + "`!at <date> <time> <to>: <message>`" + `
- ` + "`!in <n><s|m|h|d> <to>: <message>`" + `
- ` + "`!list`" + `
- ` + "`!cancel <id>`" + `
- ` + "`!ping`" + `
- ` + "`!help`" + `

_date_: today, tomorrow, DD-MM or DD-MM-YYYY
_time_: HH:MM or HH:MM:SS, 24 hours
_to_: a contact name, or a phone number with country code

Example: ` + "`!at tomorrow 09:30 Finn: Happy birthday!`"

type bot struct {
	ctx     context.Context
	cli     *whatsmeow.Client
	db      *sql.DB
	wake    chan struct{}
	started time.Time
}

// me is the control chat: the user's own "message yourself" chat.
func (b *bot) me() types.JID { return b.cli.Store.ID.ToNonAD() }

func (b *bot) handle(evt any) {
	m, ok := evt.(*events.Message)
	if !ok || m.IsEdit || !m.Info.IsFromMe || m.Info.IsGroup {
		return
	}
	if u := m.Info.Chat.User; u != b.cli.Store.ID.User && u != b.cli.Store.LID.User {
		return
	}
	text := m.Message.GetConversation()
	if text == "" {
		text = m.Message.GetExtendedTextMessage().GetText()
	}
	b.command(text, m.Info.ID)
}

func (b *bot) command(text, id string) {
	c, err := parse(text, time.Now())
	if err != nil {
		b.reply(id, text, "🤖 *Error*\n"+err.Error())
		return
	}
	switch c.kind {
	case kHelp:
		b.reply(id, text, help)
	case kPing:
		p, _ := pending(b.ctx, b.db)
		b.reply(id, text, fmt.Sprintf("🤖 *Pong*, up %s, %d scheduled", time.Since(b.started).Round(time.Second), len(p)))
	case kList:
		b.list(id, text)
	case kCancel:
		j, err := get(b.ctx, b.db, c.id)
		switch {
		case err != nil:
			b.reply(id, text, fmt.Sprintf("🤖 *Error*\nThere is no message #%d.", c.id))
		case !finish(b.ctx, b.db, c.id, "cancelled"):
			b.reply(id, text, fmt.Sprintf("🤖 *Error*\nMessage #%d is already %s.", c.id, j.status))
		default:
			b.poke()
			b.reply(id, text, "🤖 *Cancelled*\n"+card(j))
		}
	case kSchedule:
		to, err := b.resolve(c.to)
		if err != nil {
			b.reply(id, text, "🤖 *Error*\n"+err.Error())
			return
		}
		j := job{runAt: c.when, to: to.String(), label: c.to, body: c.body, srcID: id, srcMsg: text}
		if j.id, err = add(b.ctx, b.db, j); err != nil {
			b.reply(id, text, "🤖 *Error*\nCould not save: "+err.Error())
		} else if j.id != 0 {
			b.poke()
			b.reply(id, text, "🤖 *Scheduled*\n"+card(j))
		}
	}
}

func (b *bot) list(id, text string) {
	jobs, err := pending(b.ctx, b.db)
	if err != nil || len(jobs) == 0 {
		b.reply(id, text, "🤖 Nothing is scheduled.")
		return
	}
	var sb strings.Builder
	sb.WriteString("🤖 *Scheduled*")
	for _, j := range jobs {
		fmt.Fprintf(&sb, "\n- `#%d` %s, *%s*: %s", j.id, when(j.runAt), j.label, j.body)
	}
	b.reply(id, text, sb.String())
}

// run sends a due job, or reports it missed or failed, and replies to the
// command that created it.
func (b *bot) run(j job) {
	if late := time.Since(j.runAt); late > maxLate {
		if finish(b.ctx, b.db, j.id, "missed") {
			b.reply(j.srcID, j.srcMsg, "🤖 *Missed*, the worker was offline\n"+card(j))
		}
		return
	}
	to, err := types.ParseJID(j.to)
	if err == nil {
		_, err = b.cli.SendMessage(b.ctx, to, &waE2E.Message{Conversation: proto.String(j.body)})
	}
	if err != nil {
		if finish(b.ctx, b.db, j.id, "failed") {
			b.reply(j.srcID, j.srcMsg, "🤖 *Failed*\n"+card(j)+"\n_"+err.Error()+"_")
		}
	} else if finish(b.ctx, b.db, j.id, "sent") {
		b.reply(j.srcID, j.srcMsg, "🤖 *Sent*\n"+card(j))
	}
}

// reply answers in the control chat as a quote of the message it is about.
func (b *bot) reply(srcID, srcText, text string) {
	_, err := b.cli.SendMessage(b.ctx, b.me(), &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String(text),
		ContextInfo: &waE2E.ContextInfo{
			StanzaID:      proto.String(srcID),
			Participant:   proto.String(b.me().String()),
			QuotedMessage: &waE2E.Message{Conversation: proto.String(srcText)},
		},
	}})
	if err != nil {
		log.Printf("reply: %v", err)
	}
}

func when(t time.Time) string {
	if t.Second() == 0 {
		return t.Format("Mon 02-01-2006 15:04")
	}
	return t.Format("Mon 02-01-2006 15:04:05")
}

func card(j job) string {
	return fmt.Sprintf("`#%d` to *%s*, %s\n> %s", j.id, j.label, when(j.runAt), strings.ReplaceAll(j.body, "\n", "\n> "))
}

// resolve maps a phone number or a contact name to a WhatsApp JID.
func (b *bot) resolve(who string) (types.JID, error) {
	if digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, who); strings.HasPrefix(who, "+") {
		resp, err := b.cli.IsOnWhatsApp(b.ctx, []string{"+" + digits})
		if err != nil || len(resp) == 0 || !resp[0].IsIn {
			return types.JID{}, fmt.Errorf("%s is not on WhatsApp.", who)
		}
		return resp[0].JID, nil
	}
	contacts, err := b.cli.Store.Contacts.GetAllContacts(b.ctx)
	if err != nil {
		return types.JID{}, err
	}
	var exact, partial []types.JID
	for jid, c := range contacts {
		for _, n := range []string{c.FullName, c.FirstName, c.PushName, c.BusinessName} {
			if n = strings.ToLower(n); n == strings.ToLower(who) {
				exact = append(exact, jid)
				break
			} else if n != "" && strings.Contains(n, strings.ToLower(who)) {
				partial = append(partial, jid)
				break
			}
		}
	}
	for _, found := range [][]types.JID{exact, partial} {
		if len(found) == 1 {
			return found[0], nil
		} else if len(found) > 1 {
			return types.JID{}, fmt.Errorf("%q matches %d contacts, use the phone number with a +.", who, len(found))
		}
	}
	return types.JID{}, fmt.Errorf("No contact named %q, use the phone number with a +.", who)
}

// poke tells the scheduler loop to look at the schedule again.
func (b *bot) poke() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}
