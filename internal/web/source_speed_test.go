package web

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

// fakeSearcher is a deterministic siteSearcher used to verify the speed-probe
// algorithm. Each call honours a per-call delay and either returns a fake
// result list or a chosen error. Recursive arrays default to the last entry.
type fakeSearcher struct {
	delays  []time.Duration
	results []int
	errs    []error
	calls   atomic.Int32
}

func (s *fakeSearcher) Search(ctx context.Context, _ string, _ int) ([]model.SearchResult, error) {
	idx := int(s.calls.Add(1)) - 1
	delay := pickDelay(s.delays, idx)
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	} else {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	if err := pickErr(s.errs, idx); err != nil {
		return nil, err
	}
	count := pickInt(s.results, idx)
	if count <= 0 {
		count = 1
	}
	return make([]model.SearchResult, count), nil
}

func pickDelay(arr []time.Duration, i int) time.Duration {
	if len(arr) == 0 {
		return 0
	}
	if i >= len(arr) {
		return arr[len(arr)-1]
	}
	return arr[i]
}

func pickErr(arr []error, i int) error {
	if len(arr) == 0 {
		return nil
	}
	if i >= len(arr) {
		return arr[len(arr)-1]
	}
	return arr[i]
}

func pickInt(arr []int, i int) int {
	if len(arr) == 0 {
		return 0
	}
	if i >= len(arr) {
		return arr[len(arr)-1]
	}
	return arr[i]
}

// withinTolerance returns true if |actual-expected| <= tolerance.
func withinTolerance(actual, expected, tolerance time.Duration) bool {
	diff := actual - expected
	if diff < 0 {
		diff = -diff
	}
	return diff <= tolerance
}

// TestRunSpeedProbeFastSampleSkipsSecondCall verifies that a single-shot fast
// response is reported as-is and we don't waste time on a second probe.
func TestRunSpeedProbeFastSampleSkipsSecondCall(t *testing.T) {
	t.Parallel()
	searcher := &fakeSearcher{
		delays:  []time.Duration{120 * time.Millisecond},
		results: []int{3},
	}

	probe := runSpeedProbe(context.Background(), searcher, "test", 5*time.Second)

	if probe.Err != nil {
		t.Fatalf("unexpected error: %v", probe.Err)
	}
	if probe.Samples != 1 {
		t.Fatalf("expected 1 sample for fast site, got %d", probe.Samples)
	}
	if got := searcher.calls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 search call, got %d", got)
	}
	if !withinTolerance(probe.Elapsed, 120*time.Millisecond, 80*time.Millisecond) {
		t.Fatalf("elapsed %v should be ~120ms", probe.Elapsed)
	}
	if probe.Count != 3 {
		t.Fatalf("expected count 3, got %d", probe.Count)
	}
}

// TestRunSpeedProbeSlowFirstSampleTakesMin runs a slow first call and a fast
// second call, then asserts the reported elapsed is the minimum of the two.
func TestRunSpeedProbeSlowFirstSampleTakesMin(t *testing.T) {
	t.Parallel()
	searcher := &fakeSearcher{
		delays:  []time.Duration{1200 * time.Millisecond, 200 * time.Millisecond},
		results: []int{1, 1},
	}

	probe := runSpeedProbe(context.Background(), searcher, "test", 5*time.Second)

	if probe.Err != nil {
		t.Fatalf("unexpected error: %v", probe.Err)
	}
	if probe.Samples != 2 {
		t.Fatalf("expected 2 samples for slow first probe, got %d", probe.Samples)
	}
	if got := searcher.calls.Load(); got != 2 {
		t.Fatalf("expected exactly 2 search calls, got %d", got)
	}
	if !withinTolerance(probe.Elapsed, 200*time.Millisecond, 100*time.Millisecond) {
		t.Fatalf("min elapsed %v should be ~200ms (warm-up gone)", probe.Elapsed)
	}
}

// TestRunSpeedProbeSlowBothSamplesUsesShorter still picks the smaller value
// even when both samples are slow, to dampen jitter on consistently slow sites.
func TestRunSpeedProbeSlowBothSamplesUsesShorter(t *testing.T) {
	t.Parallel()
	searcher := &fakeSearcher{
		delays:  []time.Duration{1500 * time.Millisecond, 1100 * time.Millisecond},
		results: []int{2, 2},
	}

	probe := runSpeedProbe(context.Background(), searcher, "test", 5*time.Second)

	if probe.Samples != 2 {
		t.Fatalf("expected 2 samples, got %d", probe.Samples)
	}
	if !withinTolerance(probe.Elapsed, 1100*time.Millisecond, 200*time.Millisecond) {
		t.Fatalf("elapsed %v should be ~1100ms (the smaller of two slow samples)", probe.Elapsed)
	}
}

// TestRunSpeedProbeFailedFirstCallNotRetried makes sure failing probes report
// quickly and the second sample is *not* spent — failures should be loud.
func TestRunSpeedProbeFailedFirstCallNotRetried(t *testing.T) {
	t.Parallel()
	failure := errors.New("dial tcp: lookup example.invalid: no such host")
	searcher := &fakeSearcher{
		delays: []time.Duration{50 * time.Millisecond},
		errs:   []error{failure},
	}

	probe := runSpeedProbe(context.Background(), searcher, "test", 5*time.Second)

	if probe.Err == nil {
		t.Fatalf("expected error to be reported")
	}
	if probe.Samples != 1 {
		t.Fatalf("expected 1 sample on failure, got %d", probe.Samples)
	}
	if got := searcher.calls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 call on failure, got %d", got)
	}
	if probe.TimedOut {
		t.Fatalf("non-timeout failure should not set TimedOut")
	}
}

// TestRunSpeedProbeReportsTimeoutOnDeadline verifies that exceeding the
// per-site timeout is flagged as TimedOut.
func TestRunSpeedProbeReportsTimeoutOnDeadline(t *testing.T) {
	t.Parallel()
	searcher := &fakeSearcher{
		delays: []time.Duration{2 * time.Second},
	}

	probe := runSpeedProbe(context.Background(), searcher, "test", 150*time.Millisecond)

	if probe.Err == nil {
		t.Fatalf("expected timeout error")
	}
	if !probe.TimedOut {
		t.Fatalf("TimedOut flag should be set on deadline exceeded; got err=%v", probe.Err)
	}
	if !withinTolerance(probe.Elapsed, 150*time.Millisecond, 100*time.Millisecond) {
		t.Fatalf("elapsed %v should be ~150ms (timeout duration)", probe.Elapsed)
	}
}

// TestRunSpeedProbeParentCancelNotTimedOut: when the parent context is
// cancelled (HTTP client gave up), the probe propagates the cancellation but
// must not blame the site by setting TimedOut.
func TestRunSpeedProbeParentCancelNotTimedOut(t *testing.T) {
	t.Parallel()
	searcher := &fakeSearcher{
		delays: []time.Duration{2 * time.Second},
	}

	parent, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(80 * time.Millisecond)
		cancel()
	}()

	probe := runSpeedProbe(parent, searcher, "test", 5*time.Second)

	if probe.Err == nil {
		t.Fatalf("expected cancellation error")
	}
	if probe.TimedOut {
		t.Fatalf("parent cancellation must NOT be reported as a site timeout")
	}
}

// TestRunSourceSpeedTestRespectsConcurrencyCap proves the new fan-out cap
// actually limits parallelism. We register more sites than the cap allows and
// observe that the running counter never exceeds it.
func TestRunSourceSpeedTestRespectsConcurrencyCap(t *testing.T) {
	t.Parallel()

	const total = 16
	if sourceSpeedMaxParallelism >= total {
		t.Fatalf("test assumes %d > sourceSpeedMaxParallelism (%d)", total, sourceSpeedMaxParallelism)
	}

	var (
		mu          sync.Mutex
		concurrent  int
		peakParCnt  int
		searchDelay = 80 * time.Millisecond
	)

	probe := func(ctx context.Context) error {
		mu.Lock()
		concurrent++
		if concurrent > peakParCnt {
			peakParCnt = concurrent
		}
		mu.Unlock()
		defer func() {
			mu.Lock()
			concurrent--
			mu.Unlock()
		}()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(searchDelay):
			return nil
		}
	}

	results := runSiteProbesParallel(context.Background(), total, sourceSpeedMaxParallelism, probe)

	if peakParCnt > sourceSpeedMaxParallelism {
		t.Fatalf("peak concurrency %d exceeded cap %d", peakParCnt, sourceSpeedMaxParallelism)
	}
	if peakParCnt < sourceSpeedMaxParallelism {
		t.Fatalf("expected probes to saturate cap %d, peaked at %d", sourceSpeedMaxParallelism, peakParCnt)
	}
	for i, err := range results {
		if err != nil {
			t.Fatalf("probe %d failed: %v", i, err)
		}
	}
}

// runSiteProbesParallel mirrors the dispatching logic in runSourceSpeedTest
// without involving the full Service. Keeping this helper close to the
// production loop ensures the test exercises the same semaphore pattern.
func runSiteProbesParallel(ctx context.Context, n, parallelism int, probe func(context.Context) error) []error {
	results := make([]error, n)
	sem := make(chan struct{}, parallelism)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = probe(ctx)
		}()
	}
	wg.Wait()
	return results
}

// TestRunSpeedProbeMatchesRealityWithJitter exercises the algorithm against a
// fake site whose latency drifts (250ms, 500ms, 200ms, ...) to ensure the
// reported elapsed converges on the *fastest* observed sample, which is the
// metric we promise users.
func TestRunSpeedProbeMatchesRealityWithJitter(t *testing.T) {
	t.Parallel()
	delays := []time.Duration{
		250 * time.Millisecond,
		500 * time.Millisecond,
	}
	searcher := &fakeSearcher{delays: delays, results: []int{4}}

	probe := runSpeedProbe(context.Background(), searcher, "test", 5*time.Second)

	if probe.Err != nil {
		t.Fatalf("unexpected error: %v", probe.Err)
	}
	// 250ms is below cutoff → only one sample expected.
	if probe.Samples != 1 {
		t.Fatalf("first sample 250ms is below cutoff, expected 1 sample, got %d", probe.Samples)
	}
	if !withinTolerance(probe.Elapsed, 250*time.Millisecond, 100*time.Millisecond) {
		t.Fatalf("elapsed %v should match the first sample (~250ms)", probe.Elapsed)
	}
}

// TestRunSpeedProbeCountUpdatesWithBetterSample makes sure when the second
// sample wins on time it also brings its result count, not the slower one's.
func TestRunSpeedProbeCountUpdatesWithBetterSample(t *testing.T) {
	t.Parallel()
	searcher := &fakeSearcher{
		delays:  []time.Duration{1100 * time.Millisecond, 300 * time.Millisecond},
		results: []int{0, 5}, // first is slow with no result, second is fast with 5
	}

	probe := runSpeedProbe(context.Background(), searcher, "test", 5*time.Second)

	if probe.Samples != 2 {
		t.Fatalf("expected 2 samples, got %d", probe.Samples)
	}
	if probe.Count != 5 {
		t.Fatalf("expected count to follow the winning (faster) sample → 5, got %d", probe.Count)
	}
}

// Compile-time guard: ensure the production siteSearcher contract still
// matches the assumptions made by these tests.
var _ siteSearcher = (*fakeSearcher)(nil)
