package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestMemoryConcurrencyMiddlewareReturns429WhenWaitTimeoutExpires(t *testing.T) {
	gin.SetMode(gin.TestMode)

	limiter := newMemoryConcurrencyLimiter(config.MemoryConcurrencyConfig{
		Enable:             true,
		TargetMemoryMB:     256,
		InitialConcurrency: 1,
		MinConcurrency:     1,
		MaxConcurrency:     1,
		WaitTimeoutSeconds: 1,
		CheckIntervalSeconds: 60,
	})
	defer limiter.close()

	block := make(chan struct{})
	release := make(chan struct{})

	router := gin.New()
	router.Use(limiter.middleware())
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		close(block)
		<-release
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		firstDone <- rr
	}()

	<-block

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("unexpected status code: got %d want %d body=%s", rr.Code, http.StatusTooManyRequests, rr.Body.String())
	}

	close(release)
	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("first request did not finish")
	}
}

func TestMemoryConcurrencyLimiterApplyConfigReleasesWaiters(t *testing.T) {
	limiter := newMemoryConcurrencyLimiter(config.MemoryConcurrencyConfig{
		Enable:             true,
		TargetMemoryMB:     256,
		InitialConcurrency: 1,
		MinConcurrency:     1,
		MaxConcurrency:     1,
		WaitTimeoutSeconds: 10,
		CheckIntervalSeconds: 60,
	})
	defer limiter.close()

	releaseOne, err := limiter.acquire(t.Context())
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	defer releaseOne()

	acquired := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		releaseTwo, err := limiter.acquire(t.Context())
		if err != nil {
			errCh <- err
			return
		}
		defer releaseTwo()
		close(acquired)
	}()

	time.Sleep(100 * time.Millisecond)
	limiter.applyConfig(config.MemoryConcurrencyConfig{
		Enable:             true,
		TargetMemoryMB:     256,
		InitialConcurrency: 2,
		MinConcurrency:     1,
		MaxConcurrency:     2,
		WaitTimeoutSeconds: 10,
		CheckIntervalSeconds: 60,
	})

	select {
	case err := <-errCh:
		t.Fatalf("second acquire failed: %v", err)
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("second acquire did not proceed after increasing limit")
	}
}
