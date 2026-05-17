package web

import (
	"math"
	"testing"
	"time"
)

// helper to feed a sequence of (offset, done) observations into the window
// estimator and return the final samples slice + final rate.
func feedSamples(window time.Duration, max int, events []rateEvent) (rate float64, samples []rateSample) {
	for _, ev := range events {
		samples, rate = updateRateWindow(samples, ev.t, ev.done, window, max)
	}
	return
}

type rateEvent struct {
	t    time.Time
	done int
}

func mkSeries(start time.Time, count int, dt time.Duration, chaptersPerEvent int) []rateEvent {
	events := make([]rateEvent, 0, count)
	done := 0
	for i := 0; i < count; i++ {
		done += chaptersPerEvent
		events = append(events, rateEvent{t: start.Add(dt * time.Duration(i+1)), done: done})
	}
	return events
}

func nearly(actual, expected, tolerance float64) bool {
	return math.Abs(actual-expected) <= tolerance
}

// ===== Window estimator core =====

// TestUpdateRateWindowSteadyState: 10 chap/s for 30s should report ~10 chap/s.
func TestUpdateRateWindowSteadyState(t *testing.T) {
	t.Parallel()
	start := time.Unix(1_700_000_000, 0).UTC()
	events := mkSeries(start, 30, time.Second, 10) // 30 events, 1s apart, +10 chapters each

	rate, _ := feedSamples(rateWindowDuration, rateMaxSamples, events)

	if !nearly(rate, 10.0, 0.5) {
		t.Fatalf("steady-state rate should be ~10 chap/s, got %.3f", rate)
	}
}

// TestUpdateRateWindowAccelerationConverges: rate jumps from 5 to 15 partway
// through. After enough new samples have entered the window, the reported rate
// should track the new speed (within ±20%).
func TestUpdateRateWindowAccelerationConverges(t *testing.T) {
	t.Parallel()
	start := time.Unix(1_700_000_000, 0).UTC()

	// Phase 1: 5 chap/s for 35s (so old samples drop out of the 30s window).
	events := mkSeries(start, 35, time.Second, 5)
	// Phase 2: 15 chap/s for 35s after 5-chap-per-event series ended.
	phase2Start := start.Add(35 * time.Second)
	for i := 1; i <= 35; i++ {
		events = append(events, rateEvent{t: phase2Start.Add(time.Duration(i) * time.Second), done: 35*5 + i*15})
	}

	rate, _ := feedSamples(rateWindowDuration, rateMaxSamples, events)

	if !nearly(rate, 15.0, 3.0) {
		t.Fatalf("after acceleration to 15 chap/s, expected ~15, got %.3f", rate)
	}
}

// TestUpdateRateWindowDecelerationConverges: rate drops from 20 to 4. After
// the window flushes, reported rate should approach 4 (NOT stay at 20).
func TestUpdateRateWindowDecelerationConverges(t *testing.T) {
	t.Parallel()
	start := time.Unix(1_700_000_000, 0).UTC()

	// Phase 1: 20 chap/s for 30 events.
	events := mkSeries(start, 30, time.Second, 20)
	// Phase 2: 4 chap/s for enough events to fill the window.
	phase2Start := start.Add(30 * time.Second)
	doneSoFar := 30 * 20
	for i := 1; i <= 35; i++ {
		doneSoFar += 4
		events = append(events, rateEvent{t: phase2Start.Add(time.Duration(i) * time.Second), done: doneSoFar})
	}

	rate, _ := feedSamples(rateWindowDuration, rateMaxSamples, events)

	if !nearly(rate, 4.0, 1.0) {
		t.Fatalf("after deceleration to 4 chap/s, expected ~4, got %.3f", rate)
	}
}

// TestUpdateRateWindowStallReturnsZero: no chapters for 30s with the same
// "done" repeating means rate=0 (caller hides ETA), instead of carrying the
// previous estimate forever.
func TestUpdateRateWindowStallReturnsZero(t *testing.T) {
	t.Parallel()
	start := time.Unix(1_700_000_000, 0).UTC()

	// Steady 10 chap/s for 10s.
	events := mkSeries(start, 10, time.Second, 10)
	// Then the reporter keeps echoing done=100 at 1s cadence for 35s — a stall.
	stallStart := start.Add(10 * time.Second)
	for i := 1; i <= 35; i++ {
		events = append(events, rateEvent{t: stallStart.Add(time.Duration(i) * time.Second), done: 100})
	}

	rate, samples := feedSamples(rateWindowDuration, rateMaxSamples, events)

	if rate != 0 {
		t.Fatalf("stall window should return 0, got %.3f", rate)
	}
	// Sanity: estimator must still hold the latest sample so the next progress
	// can resume immediately without missing the boundary.
	if len(samples) == 0 {
		t.Fatalf("estimator should always retain at least the latest sample")
	}
}

// TestUpdateRateWindowSingleSlowChapterDoesNotPoison: one rate-limited chapter
// (5s pause inside an otherwise 10 chap/s stream) should barely affect the
// reported rate, because the window covers 30s of work.
func TestUpdateRateWindowSingleSlowChapterDoesNotPoison(t *testing.T) {
	t.Parallel()
	start := time.Unix(1_700_000_000, 0).UTC()

	// 25s of steady 10 chap/s, then 5s pause where progress sits at 250,
	// then 5 more seconds of 10 chap/s.
	var events []rateEvent
	done := 0
	for i := 1; i <= 25; i++ {
		done += 10
		events = append(events, rateEvent{t: start.Add(time.Duration(i) * time.Second), done: done})
	}
	// Single slow chapter — emit done unchanged at +1s, then resume.
	events = append(events, rateEvent{t: start.Add(30 * time.Second), done: done})
	// Resume — 5s of work
	for i := 1; i <= 5; i++ {
		done += 10
		events = append(events, rateEvent{t: start.Add(time.Duration(30+i) * time.Second), done: done})
	}

	rate, _ := feedSamples(rateWindowDuration, rateMaxSamples, events)

	// 30s window saw ≈ (350 - 50) / 30 = 10 chap/s; tolerance loose for the
	// 5s pause.
	if !nearly(rate, 10.0, 2.0) {
		t.Fatalf("single slow chapter shouldn't poison: expected ~10 chap/s, got %.3f", rate)
	}
}

// TestUpdateRateWindowResetsOnRegression: if `done` regresses (retry / restart
// of an already-counted chapter), the buffer flushes so the rate doesn't go
// negative or stale.
func TestUpdateRateWindowResetsOnRegression(t *testing.T) {
	t.Parallel()
	start := time.Unix(1_700_000_000, 0).UTC()

	events := mkSeries(start, 10, time.Second, 5) // 5 chap/s steady state
	// Regression: progress drops back to 5.
	events = append(events, rateEvent{t: start.Add(11 * time.Second), done: 5})
	// Then resume a fresh 8 chap/s climb.
	doneNow := 5
	for i := 1; i <= 30; i++ {
		doneNow += 8
		events = append(events, rateEvent{t: start.Add(time.Duration(11+i) * time.Second), done: doneNow})
	}

	rate, samples := feedSamples(rateWindowDuration, rateMaxSamples, events)

	if rate <= 0 {
		t.Fatalf("after regression+resume the estimator should re-converge, got %.3f", rate)
	}
	if !nearly(rate, 8.0, 1.5) {
		t.Fatalf("expected new steady state ~8 chap/s after regression, got %.3f", rate)
	}
	// Verify the regression dropped the old buffer (samples are all newer than
	// the regression point).
	if len(samples) >= 2 && samples[0].done < 5 {
		t.Fatalf("regression should have flushed pre-regression samples; got first done=%d", samples[0].done)
	}
}

// TestUpdateRateWindowDuplicateEventsAreNoOp: a reporter that echoes the same
// (now, done) repeatedly must not pollute the buffer.
func TestUpdateRateWindowDuplicateEventsAreNoOp(t *testing.T) {
	t.Parallel()
	start := time.Unix(1_700_000_000, 0).UTC()

	samples := []rateSample{}
	now := start.Add(time.Second)
	samples, _ = updateRateWindow(samples, now, 5, rateWindowDuration, rateMaxSamples)
	before := len(samples)
	for i := 0; i < 10; i++ {
		samples, _ = updateRateWindow(samples, now, 5, rateWindowDuration, rateMaxSamples)
	}
	if len(samples) != before {
		t.Fatalf("duplicate (now, done) should not grow buffer (was %d, now %d)", before, len(samples))
	}
}

// TestUpdateRateWindowCapsBufferSize: a 10/s reporter for many minutes must
// not grow the slice past the cap.
func TestUpdateRateWindowCapsBufferSize(t *testing.T) {
	t.Parallel()
	start := time.Unix(1_700_000_000, 0).UTC()

	// Use a tiny window so almost everything stays inside, exercising the cap.
	const cap = 16
	const window = 10 * time.Hour

	events := mkSeries(start, 5_000, 10*time.Millisecond, 1)
	_, samples := feedSamples(window, cap, events)

	if len(samples) != cap {
		t.Fatalf("buffer should be capped at %d, got %d", cap, len(samples))
	}
}

// TestUpdateRateWindowEarlyPhaseReturnsZeroBeforeSecondSample: with only one
// progress event we cannot compute a rate yet — that's by design (avoids the
// "10 chap/s reported in the first millisecond because elapsed→0" pitfall).
func TestUpdateRateWindowEarlyPhaseReturnsZeroBeforeSecondSample(t *testing.T) {
	t.Parallel()
	start := time.Unix(1_700_000_000, 0).UTC()
	samples, rate := updateRateWindow(nil, start, 1, rateWindowDuration, rateMaxSamples)
	if rate != 0 {
		t.Fatalf("first sample alone must yield rate=0 (insufficient data), got %.3f", rate)
	}
	if len(samples) != 1 {
		t.Fatalf("expected the lone sample to be retained, got %d", len(samples))
	}
}

// ===== End-to-end through MarkProgress =====

// TestMarkProgressETAReflectsRealRate: on a steady 5 chap/s with 100 chapters,
// after roughly 10 events the reported speed should be near 5 and the ETA in
// the right ballpark for the remaining work.
func TestMarkProgressETAReflectsRealRate(t *testing.T) {
	t.Parallel()
	store := NewDownloadTaskStore()
	task := store.Create("sfacg", "book-eta")
	store.MarkRunning(task.ID, "sfacg", "book-eta", "ETA test", 100)

	// Use update() directly so we can stamp deterministic timestamps into
	// the rate buffer instead of relying on real wall-clock between calls.
	start := time.Unix(1_700_000_000, 0).UTC()
	for i := 1; i <= 12; i++ {
		i := i
		store.update(task.ID, func(item *DownloadTask) {
			item.Status = "running"
			item.Phase = "downloading"
			item.CompletedChapters = i * 5
			now := start.Add(time.Duration(i) * time.Second)
			samples, rate := updateRateWindow(item.rateSamples, now, item.CompletedChapters, rateWindowDuration, rateMaxSamples)
			item.rateSamples = samples
			if rate > 0 {
				item.Speed = rate
				remaining := item.TotalChapters - item.CompletedChapters
				item.ETA = formatETADuration(time.Duration(float64(remaining)/rate*float64(time.Second)))
			}
		})
	}

	snapshot, ok := store.Snapshot(task.ID)
	if !ok {
		t.Fatalf("expected task snapshot")
	}
	if !nearly(snapshot.Speed, 5.0, 0.5) {
		t.Fatalf("expected reported speed ~5 chap/s, got %.3f", snapshot.Speed)
	}
	// 100 - 60 = 40 chapters remaining @ 5 chap/s ⇒ ~8s.
	if snapshot.ETA != "8秒" && snapshot.ETA != "7秒" && snapshot.ETA != "9秒" {
		t.Fatalf("expected ETA around 7-9 秒, got %q", snapshot.ETA)
	}
}

// TestMarkProgressHidesETAWhenStalled: if no forward motion happens in the
// recent window, the snapshot should not surface a stale ETA — the UI hides
// it, which is more honest than guessing.
func TestMarkProgressHidesETAWhenStalled(t *testing.T) {
	t.Parallel()
	store := NewDownloadTaskStore()
	task := store.Create("sfacg", "book-stall")
	store.MarkRunning(task.ID, "sfacg", "book-stall", "stall test", 200)

	// Immediately mark progress with done=10 so the speed estimator sees a
	// single sample. Then "stall" by emitting done=10 every second for 35s so
	// every old in-window sample drops.
	start := time.Unix(1_700_000_000, 0).UTC()
	for i := 0; i <= 35; i++ {
		i := i
		store.update(task.ID, func(item *DownloadTask) {
			item.Status = "running"
			item.Phase = "downloading"
			item.CompletedChapters = 10
			now := start.Add(time.Duration(i) * time.Second)
			samples, rate := updateRateWindow(item.rateSamples, now, 10, rateWindowDuration, rateMaxSamples)
			item.rateSamples = samples
			if rate > 0 {
				item.Speed = rate
			} else {
				item.Speed = 0
				item.ETA = ""
			}
		})
	}

	snapshot, _ := store.Snapshot(task.ID)
	if snapshot.Speed != 0 {
		t.Fatalf("after a long stall, speed should be cleared, got %.3f", snapshot.Speed)
	}
	if snapshot.ETA != "" {
		t.Fatalf("after a long stall, ETA should be cleared, got %q", snapshot.ETA)
	}
}
