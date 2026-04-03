package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	log "github.com/sirupsen/logrus"
)

var errMemoryConcurrencyTimeout = errors.New("memory concurrency wait timeout")

type memoryConcurrencyLimiter struct {
	mu       sync.Mutex
	cfg      config.MemoryConcurrencyConfig
	limit    int
	inflight int
	waitCh   chan struct{}
	stopCh   chan struct{}
}

func newMemoryConcurrencyLimiter(cfg config.MemoryConcurrencyConfig) *memoryConcurrencyLimiter {
	limiter := &memoryConcurrencyLimiter{
		waitCh: make(chan struct{}),
		stopCh: make(chan struct{}),
	}
	limiter.applyConfig(cfg)
	go limiter.monitorLoop()
	return limiter
}

func (l *memoryConcurrencyLimiter) middleware() gin.HandlerFunc {
	if l == nil {
		return nil
	}
	return func(c *gin.Context) {
		release, err := l.acquire(c.Request.Context())
		if err != nil {
			if errors.Is(err, errMemoryConcurrencyTimeout) {
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "memory limit"})
				return
			}
			c.Abort()
			return
		}
		defer release()
		c.Next()
	}
}

func (l *memoryConcurrencyLimiter) acquire(ctx context.Context) (func(), error) {
	if l == nil {
		return func() {}, nil
	}

	var deadline time.Time
	for {
		l.mu.Lock()
		cfg := l.cfg
		limit := l.limit
		if !cfg.Enable || limit <= 0 {
			l.mu.Unlock()
			return func() {}, nil
		}
		if l.inflight < limit {
			l.inflight++
			l.mu.Unlock()
			return func() {
				l.mu.Lock()
				if l.inflight > 0 {
					l.inflight--
				}
				l.notifyWaitersLocked()
				l.mu.Unlock()
			}, nil
		}
		waitCh := l.waitCh
		stopCh := l.stopCh
		if deadline.IsZero() {
			deadline = time.Now().Add(time.Duration(cfg.WaitTimeoutSeconds * float64(time.Second)))
		}
		remaining := time.Until(deadline)
		l.mu.Unlock()

		if remaining <= 0 {
			return nil, errMemoryConcurrencyTimeout
		}

		timer := time.NewTimer(remaining)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-timer.C:
			return nil, errMemoryConcurrencyTimeout
		case <-waitCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-stopCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return func() {}, nil
		}
	}
}

func (l *memoryConcurrencyLimiter) applyConfig(cfg config.MemoryConcurrencyConfig) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	l.cfg = cfg
	if !cfg.Enable || cfg.TargetMemoryMB <= 0 {
		l.limit = 0
		l.notifyWaitersLocked()
		return
	}

	nextLimit := cfg.InitialConcurrency
	if l.limit > 0 {
		nextLimit = l.limit
	}
	if nextLimit < cfg.MinConcurrency {
		nextLimit = cfg.MinConcurrency
	}
	if nextLimit > cfg.MaxConcurrency {
		nextLimit = cfg.MaxConcurrency
	}
	l.limit = nextLimit
	l.notifyWaitersLocked()
}

func (l *memoryConcurrencyLimiter) close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	select {
	case <-l.stopCh:
	default:
		close(l.stopCh)
	}
	l.notifyWaitersLocked()
	l.mu.Unlock()
}

func (l *memoryConcurrencyLimiter) monitorLoop() {
	if l == nil {
		return
	}

	for {
		interval := l.currentInterval()
		timer := time.NewTimer(interval)
		select {
		case <-timer.C:
		case <-l.stopCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		}

		cfg := l.currentConfig()
		if !cfg.Enable || cfg.TargetMemoryMB <= 0 {
			continue
		}

		rssBytes, err := currentProcessRSSBytes()
		if err != nil {
			log.Debugf("memory concurrency: failed to read RSS: %v", err)
			continue
		}

		targetBytes := int64(cfg.TargetMemoryMB) * 1024 * 1024
		l.adjustLimit(rssBytes, targetBytes)
	}
}

func (l *memoryConcurrencyLimiter) currentConfig() config.MemoryConcurrencyConfig {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.cfg
}

func (l *memoryConcurrencyLimiter) currentInterval() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cfg.CheckIntervalSeconds <= 0 {
		return time.Second
	}
	return time.Duration(l.cfg.CheckIntervalSeconds) * time.Second
}

func (l *memoryConcurrencyLimiter) adjustLimit(rssBytes, targetBytes int64) {
	l.mu.Lock()
	defer l.mu.Unlock()

	cfg := l.cfg
	if !cfg.Enable || cfg.TargetMemoryMB <= 0 {
		return
	}

	nextLimit := l.limit
	switch {
	case rssBytes > targetBytes && nextLimit > cfg.MinConcurrency:
		nextLimit--
	case rssBytes < targetBytes && nextLimit < cfg.MaxConcurrency:
		nextLimit++
	default:
		return
	}

	if nextLimit == l.limit {
		return
	}
	l.limit = nextLimit
	l.notifyWaitersLocked()
	log.Debugf(
		"memory concurrency adjusted: rss=%d target=%d limit=%d inflight=%d",
		rssBytes,
		targetBytes,
		l.limit,
		l.inflight,
	)
}

func (l *memoryConcurrencyLimiter) notifyWaitersLocked() {
	close(l.waitCh)
	l.waitCh = make(chan struct{})
}

func currentProcessRSSBytes() (int64, error) {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, errors.New("VmRSS entry missing value")
		}
		valueKB, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0, err
		}
		return valueKB * 1024, nil
	}
	return 0, errors.New("VmRSS not found")
}
