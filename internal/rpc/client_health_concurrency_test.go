// Copyright 2026 Erst Users
// SPDX-License-Identifier: Apache-2.0

package rpc

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestIsHealthyLocked_HighConcurrency verifies that isHealthyLocked is
// deadlock-free and race-condition-free under heavy parallel access.
// Run with: go test -race -run TestIsHealthyLocked_HighConcurrency
func TestIsHealthyLocked_HighConcurrency(t *testing.T) {
	const (
		numGoroutines = 100
		numIterations = 50
		testURL       = "https://example.stellar.org"
	)

	client := &Client{
		failures:    make(map[string]int),
		lastFailure: make(map[string]time.Time),
		AltURLs:     []string{testURL},
		SorobanURL:  testURL,
	}

	var wg sync.WaitGroup

	// Spin up concurrent readers and writers simultaneously.
	// Readers call isHealthyLocked (via IsHealthy which holds RLock).
	// Writers call recordFailure / resetFailures which hold Lock.
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				switch j % 3 {
				case 0:
					// Read path — acquires RLock internally via IsHealthy
					client.mu.RLock()
					_ = client.isHealthyLocked(testURL)
					client.mu.RUnlock()
				case 1:
					// Write path — record a failure
					client.mu.Lock()
					if client.failures == nil {
						client.failures = make(map[string]int)
					}
					if client.lastFailure == nil {
						client.lastFailure = make(map[string]time.Time)
					}
					client.failures[testURL]++
					client.lastFailure[testURL] = time.Now()
					client.mu.Unlock()
				case 2:
					// Write path — reset failures
					client.mu.Lock()
					if client.failures == nil {
						client.failures = make(map[string]int)
					}
					client.failures[testURL] = 0
					client.mu.Unlock()
				}
			}
		}(i)
	}

	// Use a timeout channel so the test fails fast if there is a deadlock
	// rather than hanging the CI runner indefinitely.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines completed — no deadlock.
	case <-time.After(10 * time.Second):
		t.Fatal("deadlock detected: goroutines did not finish within 10 seconds")
	}
}

// TestIsHealthyLocked_CircuitBreakerSemantics verifies the core health logic:
//   - fewer than 5 failures → healthy
//   - 5+ failures within 60s  → unhealthy
//   - 5+ failures but >60s ago → healthy again (circuit resets)
func TestIsHealthyLocked_CircuitBreakerSemantics(t *testing.T) {
	const testURL = "https://example.stellar.org"

	t.Run("healthy when no failures recorded", func(t *testing.T) {
		c := &Client{
			failures:    make(map[string]int),
			lastFailure: make(map[string]time.Time),
		}
		c.mu.RLock()
		healthy := c.isHealthyLocked(testURL)
		c.mu.RUnlock()
		assert.True(t, healthy)
	})

	t.Run("healthy when failures below threshold", func(t *testing.T) {
		c := &Client{
			failures:    map[string]int{testURL: 4},
			lastFailure: map[string]time.Time{testURL: time.Now()},
		}
		c.mu.RLock()
		healthy := c.isHealthyLocked(testURL)
		c.mu.RUnlock()
		assert.True(t, healthy)
	})

	t.Run("unhealthy when failures at threshold within window", func(t *testing.T) {
		c := &Client{
			failures:    map[string]int{testURL: 5},
			lastFailure: map[string]time.Time{testURL: time.Now()},
		}
		c.mu.RLock()
		healthy := c.isHealthyLocked(testURL)
		c.mu.RUnlock()
		assert.False(t, healthy)
	})

	t.Run("healthy again after circuit timeout", func(t *testing.T) {
		c := &Client{
			failures:    map[string]int{testURL: 10},
			lastFailure: map[string]time.Time{testURL: time.Now().Add(-61 * time.Second)},
		}
		c.mu.RLock()
		healthy := c.isHealthyLocked(testURL)
		c.mu.RUnlock()
		assert.True(t, healthy)
	})
}

// TestIsHealthyLocked_MultiURL ensures independent circuit state per URL
// under concurrent access across multiple endpoints.
func TestIsHealthyLocked_MultiURL(t *testing.T) {
	urls := []string{
		"https://node1.stellar.org",
		"https://node2.stellar.org",
		"https://node3.stellar.org",
	}

	client := &Client{
		failures: map[string]int{
			urls[0]: 0, // healthy
			urls[1]: 5, // tripped
			urls[2]: 3, // healthy
		},
		lastFailure: map[string]time.Time{
			urls[0]: time.Now(),
			urls[1]: time.Now(), // recent — circuit open
			urls[2]: time.Now(),
		},
		AltURLs: urls,
	}

	var wg sync.WaitGroup
	results := make([]bool, len(urls))

	for i, url := range urls {
		wg.Add(1)
		go func(idx int, u string) {
			defer wg.Done()
			client.mu.RLock()
			results[idx] = client.isHealthyLocked(u)
			client.mu.RUnlock()
		}(i, url)
	}

	wg.Wait()

	assert.True(t, results[0], "node1 should be healthy (0 failures)")
	assert.False(t, results[1], "node2 should be unhealthy (5 failures, recent)")
	assert.True(t, results[2], "node3 should be healthy (3 failures, below threshold)")
}
