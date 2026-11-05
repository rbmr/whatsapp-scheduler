package main

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Amsterdam")
	now := time.Date(2026, 11, 5, 20, 0, 0, 0, loc)
	at := func(y int, m time.Month, d, h, mi, s int) time.Time { return time.Date(y, m, d, h, mi, s, 0, loc) }

	tests := []struct {
		in   string
		kind kind
		when time.Time
		to   string
		bad  bool
	}{
		{in: "just a note"},
		{in: "!ping", kind: kPing},
		{in: "!PING", kind: kPing},
		{in: "!help", kind: kHelp},
		{in: "!list", kind: kList},
		{in: "!cancel 3", kind: kCancel},
		{in: "!cancel x", bad: true},
		{in: "!bogus", bad: true},

		{in: "!at today 23:00 Finn: hi", kind: kSchedule, when: at(2026, 11, 5, 23, 0, 0), to: "Finn"},
		{in: "!at today 19:00 Finn: hi", bad: true},
		{in: "!at tomorrow 09:30:15 Finn van Oosterhout: hi", kind: kSchedule, when: at(2026, 11, 6, 9, 30, 15), to: "Finn van Oosterhout"},
		{in: "!at 24-12 18:00 +31612345678: hi", kind: kSchedule, when: at(2026, 12, 24, 18, 0, 0), to: "+31612345678"},
		{in: "!at 01-11 10:00 Finn: hi", kind: kSchedule, when: at(2027, 11, 1, 10, 0, 0), to: "Finn"},
		{in: "!at 29-02 10:00 Finn: hi", kind: kSchedule, when: at(2028, 2, 29, 10, 0, 0), to: "Finn"},
		{in: "!at 06-01-2027 08:00 Finn: hi", kind: kSchedule, when: at(2027, 1, 6, 8, 0, 0), to: "Finn"},
		{in: "!at 31-04 10:00 Finn: hi", bad: true},
		{in: "!at 2026-11-06 09:30 Finn: hi", bad: true},
		{in: "!at tomorrow 9:30 Finn: hi", bad: true},
		{in: "!at tomorrow 09:30 Finn hi", bad: true},
		{in: "!at tomorrow 09:30 : hi", bad: true},

		{in: "!in 30m Finn: hi", kind: kSchedule, when: now.Add(30 * time.Minute), to: "Finn"},
		{in: "!in 45s Finn: hi", kind: kSchedule, when: now.Add(45 * time.Second), to: "Finn"},
		{in: "!in 2d Finn: a: b", kind: kSchedule, when: now.Add(48 * time.Hour), to: "Finn"},
		{in: "!in 2 h Finn: hi", bad: true},
		{in: "!in 1h30m Finn: hi", bad: true},
		{in: "!in 2hours Finn: hi", bad: true},
		{in: "!in 0s Finn: hi", bad: true},
	}
	for _, tc := range tests {
		got, err := parse(tc.in, now)
		if (err != nil) != tc.bad || got.kind != tc.kind || got.to != tc.to || !got.when.Equal(tc.when) {
			t.Errorf("parse(%q) = %+v, %v; want kind %v when %v to %q bad %v", tc.in, got, err, tc.kind, tc.when, tc.to, tc.bad)
		}
	}
	if c, _ := parse("!in 2d Finn: a: b", now); c.body != "a: b" {
		t.Errorf("body = %q, want %q", c.body, "a: b")
	}
}
