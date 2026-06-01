// Copyright 2026 Cathryn Lavery and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"testing"
	"time"
)

func TestResolveWorkflowWindowTreatsRelativeToAsInclusiveDate(t *testing.T) {
	loc := time.UTC

	window, err := resolveWorkflowWindow("", "today", "+1d", loc)
	if err != nil {
		t.Fatalf("resolveWorkflowWindow: %v", err)
	}

	if got := window.To.Sub(window.From); got != 48*time.Hour {
		t.Fatalf("window duration = %s, want 48h for today through +1d", got)
	}
	if window.ToDate != window.To.Add(-time.Nanosecond).Format("2006-01-02") {
		t.Fatalf("window ToDate = %q, want last included date", window.ToDate)
	}
}

func TestWorkflowWindowToDateIsLastIncludedDate(t *testing.T) {
	loc := time.UTC
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, loc)
	end := time.Date(2026, 6, 2, 0, 0, 0, 0, loc)

	window := workflowWindow(start, end)
	if window.ToDate != "2026-06-01" {
		t.Fatalf("ToDate = %q, want last included date", window.ToDate)
	}
}

func TestFilterBookingsInWindowSkipsUnparseableStartsAt(t *testing.T) {
	loc := time.UTC
	window := tidycalWindow{
		From: time.Date(2026, 6, 1, 0, 0, 0, 0, loc),
		To:   time.Date(2026, 6, 2, 0, 0, 0, 0, loc),
	}
	bookings := []workflowBooking{
		{ID: "bad", StartsAt: ""},
		{ID: "outside", StartsAt: "2026-06-03T10:00:00Z"},
		{ID: "inside", StartsAt: "2026-06-01T10:00:00Z"},
	}

	got := filterBookingsInWindow(bookings, window, loc, true)
	if len(got) != 1 || got[0].ID != "inside" {
		t.Fatalf("filtered bookings = %+v, want only inside booking", got)
	}
}

func TestBuildFollowupsKeepsCancelledReasonAheadOfIntakeAnswer(t *testing.T) {
	got := buildFollowups([]workflowBooking{
		{
			ID:          "cancelled",
			CancelledAt: "2026-06-01T10:00:00Z",
			Questions:   []bookingQuestion{{Question: "Anything else?", Answer: "Please follow up"}},
		},
	})

	if len(got) != 1 {
		t.Fatalf("followups len = %d, want 1", len(got))
	}
	if got[0].SuggestedReason != "cancelled_booking" {
		t.Fatalf("SuggestedReason = %q, want cancelled_booking", got[0].SuggestedReason)
	}
}

func TestWorkflowAPIDateTimeUsesUTCDateTime(t *testing.T) {
	loc := time.FixedZone("test", -5*60*60)
	got := workflowAPIDateTime(time.Date(2026, 6, 1, 9, 30, 0, 0, loc))
	if got != "2026-06-01T14:30:00Z" {
		t.Fatalf("workflowAPIDateTime = %q, want UTC date-time", got)
	}
}

func TestFilterSlotsInWindowClipsOutOfRangeSlots(t *testing.T) {
	loc := time.UTC
	window := workflowWindow(
		time.Date(2026, 6, 1, 0, 0, 0, 0, loc),
		time.Date(2026, 6, 2, 0, 0, 0, 0, loc),
	)
	slots := []tidycalSlot{
		{StartsAt: "before", localStart: time.Date(2026, 5, 31, 23, 0, 0, 0, loc)},
		{StartsAt: "inside", localStart: time.Date(2026, 6, 1, 12, 0, 0, 0, loc)},
		{StartsAt: "boundary", localStart: time.Date(2026, 6, 2, 0, 0, 0, 0, loc)},
	}

	got := filterSlotsInWindow(slots, window, false)
	if len(got) != 1 || got[0].StartsAt != "inside" {
		t.Fatalf("filtered slots = %+v, want only inside slot", got)
	}
}
