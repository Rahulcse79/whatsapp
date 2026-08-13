package domain

import (
	"testing"
	"time"
)

func TestDayBucketing_IsUTC(t *testing.T) {
	// Late-evening UTC still buckets to that UTC calendar day.
	at := time.Date(2026, 8, 13, 23, 59, 0, 0, time.UTC)
	if DayKey(at) != "2026-08-13" {
		t.Fatalf("DayKey = %s", DayKey(at))
	}
	if !Day(at).Equal(time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("Day = %v, want midnight UTC", Day(at))
	}
}

func TestTrailingDays(t *testing.T) {
	day := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	got := TrailingDays(day, MAUWindow)
	if len(got) != 30 {
		t.Fatalf("len = %d, want 30", len(got))
	}
	if !got[0].Equal(day) {
		t.Errorf("first bucket = %v, want today (most recent)", got[0])
	}
	if !got[29].Equal(day.AddDate(0, 0, -29)) {
		t.Errorf("last bucket = %v, want 29 days back", got[29])
	}
}

func TestCounterMetric(t *testing.T) {
	if m, ok := KindSignup.CounterMetric(""); !ok || m != "signups" {
		t.Errorf("signup → (%s, %v)", m, ok)
	}
	if m, ok := KindMessageRelayed.CounterMetric(""); !ok || m != "messages_relayed" {
		t.Errorf("message_relayed → (%s, %v)", m, ok)
	}
	if m, ok := KindFlagExposure.CounterMetric("dark_mode"); !ok || m != "flag_exposure:dark_mode" {
		t.Errorf("flag(dark_mode) → (%s, %v)", m, ok)
	}
	if m, ok := KindFlagExposure.CounterMetric(""); !ok || m != "flag_exposure" {
		t.Errorf("flag(no label) → (%s, %v)", m, ok)
	}
	if _, ok := KindActiveUser.CounterMetric(""); ok {
		t.Error("active_user is distinct, must not be a counter metric")
	}
}

func TestKindClassification(t *testing.T) {
	if !KindActiveUser.Distinct() || KindSignup.Distinct() {
		t.Error("only active_user is a distinct kind")
	}
	if !KindCallMinutes.Known() || EventKind("nope").Known() {
		t.Error("Known must accept real kinds and reject unknown ones")
	}
}

func TestRetentionCutoff(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	want := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -RollupRetentionDays)
	if !RetentionCutoff(now).Equal(want) {
		t.Fatalf("cutoff = %v, want %v", RetentionCutoff(now), want)
	}
}
