package site

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestSiteRetryBackoff(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		attempt int
		min     time.Duration
		max     time.Duration
	}{
		{"429 第一次", errors.New("http 429 for https://x/1.html"), 0, 2 * time.Second, 4 * time.Second},
		{"429 第二次", errors.New("http 429 for https://x/1.html"), 1, 4 * time.Second, 6 * time.Second},
		{"429 第三次", errors.New("http 429 for https://x/1.html"), 2, 8 * time.Second, 10 * time.Second},
		{"超时用默认退避", errors.New("context deadline exceeded"), 0, time.Second, time.Second + 1},
		{"500 用默认退避", errors.New("http 500"), 1, 2 * time.Second, 2*time.Second + 1},
	}
	for _, c := range cases {
		got := siteRetryBackoff(c.err, c.attempt)
		if got < c.min || got > c.max {
			t.Fatalf("%s: backoff=%v, want in [%v, %v]", c.name, got, c.min, c.max)
		}
	}
}

func TestSiteRetryBackoffIsJittered(t *testing.T) {
	// 并发下载时多个请求同时被限流，固定间隔会让它们一起重试，
	// 抖动用于错开重试时刻，因此多次采样不应完全相同
	err := errors.New("http 429 for https://x/1.html")
	seen := make(map[time.Duration]bool)
	for i := 0; i < 20; i++ {
		seen[siteRetryBackoff(err, 0)] = true
	}
	if len(seen) < 2 {
		t.Fatalf("expected jittered backoff, got %d distinct values", len(seen))
	}
}

// withFastBackoff 在测试期间用极短退避替换真实退避，避免单测等待数秒
func withFastBackoff(t *testing.T) {
	t.Helper()
	orig := siteRetryBackoff
	siteRetryBackoff = func(error, int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { siteRetryBackoff = orig })
}

func TestGetWithSiteRetry(t *testing.T) {
	withFastBackoff(t)

	t.Run("首次成功不重试", func(t *testing.T) {
		calls := 0
		got, err := getWithSiteRetry(context.Background(), func() (string, error) {
			calls++
			return "ok", nil
		}, defaultSiteRetryAttempts)
		if err != nil || got != "ok" || calls != 1 {
			t.Fatalf("got=%q err=%v calls=%d", got, err, calls)
		}
	})

	t.Run("可重试错误重试后成功", func(t *testing.T) {
		calls := 0
		got, err := getWithSiteRetry(context.Background(), func() (string, error) {
			calls++
			if calls < 3 {
				return "", errors.New("http 429 for https://x/1.html")
			}
			return "recovered", nil
		}, defaultSiteRetryAttempts)
		if err != nil || got != "recovered" || calls != 3 {
			t.Fatalf("got=%q err=%v calls=%d", got, err, calls)
		}
	})

	t.Run("不可重试错误立即返回", func(t *testing.T) {
		calls := 0
		_, err := getWithSiteRetry(context.Background(), func() (string, error) {
			calls++
			return "", errors.New("http 404 not found")
		}, defaultSiteRetryAttempts)
		if err == nil || calls != 1 {
			t.Fatalf("expected immediate failure, err=%v calls=%d", err, calls)
		}
	})

	t.Run("重试耗尽返回最后错误", func(t *testing.T) {
		calls := 0
		_, err := getWithSiteRetry(context.Background(), func() (string, error) {
			calls++
			return "", errors.New("http 429 for https://x/1.html")
		}, 3)
		if err == nil || calls != 3 {
			t.Fatalf("expected exhausted retries, err=%v calls=%d", err, calls)
		}
	})

	t.Run("上下文取消立即停止", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		_, err := getWithSiteRetry(ctx, func() (string, error) {
			calls++
			cancel()
			return "", errors.New("http 429 for https://x/1.html")
		}, defaultSiteRetryAttempts)
		if err == nil || calls != 1 {
			t.Fatalf("expected stop after cancel, err=%v calls=%d", err, calls)
		}
	})
}

var _ = fmt.Sprintf

func TestRetryWithSiteBackoffGeneric(t *testing.T) {
	withFastBackoff(t)
	// 章节级重试：返回类型非字符串
	calls := 0
	got, err := retryWithSiteBackoff(context.Background(), func() (int, error) {
		calls++
		if calls < 2 {
			return 0, errors.New("http 429 for https://x/1.html")
		}
		return 42, nil
	}, defaultSiteRetryAttempts, shouldRetrySiteRequest)
	if err != nil || got != 42 || calls != 2 {
		t.Fatalf("got=%d err=%v calls=%d", got, err, calls)
	}
}
