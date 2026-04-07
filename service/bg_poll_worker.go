package service

import (
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/basegate"
)

// BgPollWorkerConfig configures the poll worker.
type BgPollWorkerConfig struct {
	Interval       time.Duration // how often to scan for pollable attempts
	BatchSize      int           // max attempts per scan
	MaxConcurrency int           // max concurrent poll goroutines
}

var DefaultPollConfig = BgPollWorkerConfig{
	Interval:       5 * time.Second,
	BatchSize:      50,
	MaxConcurrency: 10,
}

// BgPollWorker is the background worker that polls async attempts.
type BgPollWorker struct {
	config  BgPollWorkerConfig
	stopCh  chan struct{}
	running bool
	mu      sync.Mutex
}

// NewBgPollWorker creates a new poll worker with the given config.
func NewBgPollWorker(config BgPollWorkerConfig) *BgPollWorker {
	return &BgPollWorker{
		config: config,
		stopCh: make(chan struct{}),
	}
}

// Start begins the polling loop in a background goroutine.
func (w *BgPollWorker) Start() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running {
		return
	}
	w.running = true
	go w.loop()
	common.SysLog("bg_poll_worker: started")
}

// Stop signals the worker to stop and waits for completion.
func (w *BgPollWorker) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.running {
		return
	}
	close(w.stopCh)
	w.running = false
	common.SysLog("bg_poll_worker: stopped")
}

func (w *BgPollWorker) loop() {
	ticker := time.NewTicker(w.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.scan()
		}
	}
}

func (w *BgPollWorker) scan() {
	now := time.Now().Unix()
	attempts, err := model.GetPollableAttempts(now, w.config.BatchSize)
	if err != nil {
		common.SysError(fmt.Sprintf("bg_poll_worker: failed to get pollable attempts: %v", err))
		return
	}

	if len(attempts) == 0 {
		return
	}

	common.SysLog(fmt.Sprintf("bg_poll_worker: found %d pollable attempts", len(attempts)))

	// Semaphore for concurrency control
	sem := make(chan struct{}, w.config.MaxConcurrency)
	var wg sync.WaitGroup

	for i := range attempts {
		attempt := attempts[i] // copy for goroutine

		sem <- struct{}{} // acquire
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }() // release

			w.pollAttempt(&attempt)
		}()
	}

	wg.Wait()
}

func (w *BgPollWorker) pollAttempt(attempt *model.BgResponseAttempt) {
	// 1. Lookup adapter
	adapter := basegate.LookupAdapter(w.resolveModelFromResponse(attempt.ResponseID))
	if adapter == nil {
		common.SysError(fmt.Sprintf("bg_poll_worker: no adapter for attempt %s", attempt.AttemptID))
		return
	}

	// 2. Call Poll
	result, err := adapter.Poll(attempt.ProviderRequestID)
	if err != nil {
		// For legacy wrappers, poll is handled differently
		if basegate.IsLegacyTaskWrapper(adapter) {
			w.pollLegacyAttempt(attempt)
			return
		}
		common.SysError(fmt.Sprintf("bg_poll_worker: poll failed for %s: %v", attempt.AttemptID, err))
		return
	}

	// 3. Convert to provider event
	event := ProviderEvent{
		Status:            result.Status,
		ProviderRequestID: result.ProviderRequestID,
		PollAfterMs:       result.PollAfterMs,
	}

	if len(result.Output) > 0 {
		output := make([]interface{}, len(result.Output))
		for i, o := range result.Output {
			output[i] = map[string]interface{}{
				"type":    o.Type,
				"content": o.Content,
			}
		}
		event.Output = output
	}

	if result.Error != nil {
		event.Error = map[string]interface{}{
			"code":    result.Error.Code,
			"message": result.Error.Message,
		}
	}

	if result.RawUsage != nil {
		event.RawUsage = map[string]interface{}{
			"prompt_tokens":     result.RawUsage.PromptTokens,
			"completion_tokens": result.RawUsage.CompletionTokens,
			"total_tokens":      result.RawUsage.TotalTokens,
		}
	}

	// 4. Apply state machine
	if err := ApplyProviderEvent(attempt.ResponseID, attempt.AttemptID, event); err != nil {
		common.SysError(fmt.Sprintf("bg_poll_worker: failed to apply event for %s: %v", attempt.AttemptID, err))
	}
}

// pollLegacyAttempt handles polling for legacy task adaptor wrappers.
// These need the full Task context (base URL, API key) which is stored
// in the legacy Task table. This bridges the two systems during migration.
func (w *BgPollWorker) pollLegacyAttempt(attempt *model.BgResponseAttempt) {
	// TODO: Phase 2 - Bridge to existing task polling loop
	// For now, legacy tasks continue to be polled by the existing UpdateTaskBulk cron.
	// This will be replaced once the full bridge is implemented.
	common.SysLog(fmt.Sprintf("bg_poll_worker: legacy attempt %s deferred to existing poll loop", attempt.AttemptID))
}

// resolveModelFromResponse loads the model name from the response record.
func (w *BgPollWorker) resolveModelFromResponse(responseID string) string {
	resp, err := model.GetBgResponseByResponseID(responseID)
	if err != nil {
		return ""
	}
	return resp.Model
}

// PollAttemptOnce manually polls a single attempt. Used for testing and one-off polling.
func PollAttemptOnce(attemptID string) error {
	attempt, err := model.GetBgAttemptByAttemptID(attemptID)
	if err != nil {
		return fmt.Errorf("attempt %s not found: %w", attemptID, err)
	}
	worker := NewBgPollWorker(DefaultPollConfig)
	worker.pollAttempt(attempt)
	return nil
}
