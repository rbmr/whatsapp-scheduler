package main

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type kind int

const (
	kNone kind = iota
	kHelp
	kPing
	kList
	kCancel
	kSchedule
)

type command struct {
	kind     kind
	id       int64     // kCancel
	when     time.Time // kSchedule
	to, body string    // kSchedule
}

const (
	usageAt = "Usage: `!at <date> <time> <to>: <message>`, for example `!at tomorrow 09:30 Finn: Happy birthday!`"
	usageIn = "Usage: `!in <n><s|m|h|d> <to>: <message>`, for example `!in 2h Finn: On my way`"
)

// parse reads one chat message. Messages that do not start with "!" are not
// commands and give kNone. Error texts are written for the user.
func parse(text string, now time.Time) (command, error) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "!") {
		return command{}, nil
	}
	name, rest, _ := strings.Cut(text[1:], " ")
	rest = strings.TrimSpace(rest)

	var when time.Time
	var err error
	switch strings.ToLower(name) {
	case "help":
		return command{kind: kHelp}, nil
	case "ping":
		return command{kind: kPing}, nil
	case "list":
		return command{kind: kList}, nil
	case "cancel":
		id, err := strconv.ParseInt(strings.TrimPrefix(rest, "#"), 10, 64)
		if err != nil {
			return command{}, errors.New("Usage: `!cancel <id>`")
		}
		return command{kind: kCancel, id: id}, nil
	case "at":
		date, rest1, _ := strings.Cut(rest, " ")
		clock, rest2, _ := strings.Cut(strings.TrimSpace(rest1), " ")
		rest = rest2
		if when, err = parseAt(date, clock, now); err != nil {
			return command{}, fmt.Errorf("%w\n%s", err, usageAt)
		}
	case "in":
		span, rest1, _ := strings.Cut(rest, " ")
		rest = rest1
		if when, err = parseIn(span, now); err != nil {
			return command{}, fmt.Errorf("%w\n%s", err, usageIn)
		}
	default:
		return command{}, fmt.Errorf("Unknown command `!%s`. Send `!help` for the list.", name)
	}

	if !when.After(now) {
		return command{}, errors.New("That time is in the past.")
	}
	to, body, ok := strings.Cut(rest, ":")
	to, body = strings.TrimSpace(to), strings.TrimSpace(body)
	if !ok || to == "" || body == "" {
		return command{}, errors.New("Put the recipient, a colon and then the message.")
	}
	return command{kind: kSchedule, when: when, to: to, body: body}, nil
}

// parseAt reads a date (today, tomorrow, DD-MM or DD-MM-YYYY) and a 24-hour
// time (HH:MM or HH:MM:SS) in now's timezone. Without a year, the earliest year
// that puts the moment in the future is used.
func parseAt(date, clock string, now time.Time) (time.Time, error) {
	layout := "02-01-2006 15:04:05"
	if len(clock) == 5 {
		layout = "02-01-2006 15:04"
	}
	at := func(date string) (time.Time, error) {
		return time.ParseInLocation(layout, date+" "+clock, now.Location())
	}
	switch strings.ToLower(date) {
	case "today":
		date = now.Format("02-01-2006")
	case "tomorrow":
		date = now.AddDate(0, 0, 1).Format("02-01-2006")
	}
	if len(date) == 5 {
		for y := now.Year(); y <= now.Year()+8; y++ { // 8 years reaches the next 29 February
			if t, err := at(fmt.Sprintf("%s-%d", date, y)); err == nil && t.After(now) {
				return t, nil
			}
		}
		return time.Time{}, errors.New("That is not a valid date and time.")
	}
	t, err := at(date)
	if err != nil {
		return time.Time{}, errors.New("That is not a valid date and time.")
	}
	return t, nil
}

var spanRe = regexp.MustCompile(`^(\d{1,4})([smhd])$`)

func parseIn(span string, now time.Time) (time.Time, error) {
	m := spanRe.FindStringSubmatch(span)
	if m == nil {
		return time.Time{}, errors.New("That is not a valid duration.")
	}
	n, _ := strconv.Atoi(m[1])
	unit := map[string]time.Duration{"s": time.Second, "m": time.Minute, "h": time.Hour, "d": 24 * time.Hour}[m[2]]
	return now.Add(time.Duration(n) * unit), nil
}
