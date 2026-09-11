package model

import (
	"strings"
	"testing"
	"time"
)

func eventReferenceICS(properties string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//calterm//test//EN\r\n" +
		"BEGIN:VEVENT\r\nUID:invite-1\r\n" + properties +
		"END:VEVENT\r\nEND:VCALENDAR\r\n"
}

func TestParseEventReferenceWithEuropeBerlinTZID(t *testing.T) {
	ref, err := ParseEventReference(strings.NewReader(eventReferenceICS(
		"DTSTART;TZID=Europe/Berlin:20260915T130000\r\n"+
			"DTEND;TZID=Europe/Berlin:20260915T134500\r\nSUMMARY:Planning session\r\n"+
			"URL:https://example.com/meeting?id=123\r\n",
	)), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	berlin := berlin(t)
	if got := ref.Start.In(berlin).Format("2006-01-02 15:04"); got != "2026-09-15 13:00" {
		t.Errorf("start = %s, want 2026-09-15 13:00 Europe/Berlin", got)
	}
	if ref.Timezone != "Europe/Berlin" || ref.Summary != "Planning session" {
		t.Errorf("parsed reference = %+v", ref)
	}
	if ref.URL != "https://example.com/meeting?id=123" {
		t.Errorf("URL = %q", ref.URL)
	}
}

func TestParseEventReferenceUTC(t *testing.T) {
	ref, err := ParseEventReference(strings.NewReader(eventReferenceICS(
		"DTSTART:20260915T110000Z\r\nDTEND:20260915T114500Z\r\nSUMMARY:UTC event\r\n",
	)), berlin(t))
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 9, 15, 11, 0, 0, 0, time.UTC)
	if !ref.Start.Equal(want) || ref.Timezone != "UTC" {
		t.Errorf("start=%v timezone=%q, want %v UTC", ref.Start, ref.Timezone, want)
	}
}

func TestParseEventReferenceAllDay(t *testing.T) {
	ref, err := ParseEventReference(strings.NewReader(eventReferenceICS(
		"DTSTART;VALUE=DATE:20260914\r\nDTEND;VALUE=DATE:20260919\r\nSUMMARY:Vacation\r\n",
	)), berlin(t))
	if err != nil {
		t.Fatal(err)
	}
	if !ref.AllDay || ref.Timezone != "" {
		t.Errorf("allDay=%v timezone=%q", ref.AllDay, ref.Timezone)
	}
	if got := ref.End.Sub(ref.Start); got != 5*24*time.Hour {
		t.Errorf("duration = %v, want 5 days", got)
	}
}

func TestParseEventReferenceUnfoldsLines(t *testing.T) {
	ref, err := ParseEventReference(strings.NewReader(eventReferenceICS(
		"DTSTART:20260915T110000Z\r\nSUMMARY:A deliberately long status \r\n sync\r\n",
	)), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Summary != "A deliberately long status sync" {
		t.Errorf("summary = %q", ref.Summary)
	}
}

func TestParseEventReferenceReadsRecurrenceID(t *testing.T) {
	ref, err := ParseEventReference(strings.NewReader(eventReferenceICS(
		"RECURRENCE-ID;TZID=Europe/Berlin:20260915T130000\r\n"+
			"DTSTART;TZID=Europe/Berlin:20260915T140000\r\nSUMMARY:Moved sync\r\n",
	)), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	want := RecurrenceKey(time.Date(2026, 9, 15, 13, 0, 0, 0, berlin(t)))
	if ref.RecurrenceID != want || !ref.Recurring {
		t.Errorf("recurrenceID=%q recurring=%v, want %q true", ref.RecurrenceID, ref.Recurring, want)
	}
}

func TestParseEventReferenceRejectsBrokenICSAndMultipleEvents(t *testing.T) {
	tests := []struct {
		name string
		ics  string
	}{
		{"broken", "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:x\r\n"},
		{"multiple VEVENTs", "BEGIN:VCALENDAR\r\nVERSION:2.0\r\n" +
			"BEGIN:VEVENT\r\nUID:a\r\nDTSTART:20260915T110000Z\r\nEND:VEVENT\r\n" +
			"BEGIN:VEVENT\r\nUID:b\r\nDTSTART:20260916T110000Z\r\nEND:VEVENT\r\n" +
			"END:VCALENDAR\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseEventReference(strings.NewReader(tt.ics), time.UTC); err == nil {
				t.Fatal("ParseEventReference returned nil error")
			}
		})
	}
}
