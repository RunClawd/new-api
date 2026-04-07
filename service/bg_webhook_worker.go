package service

import (
	"bytes"
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

type BgWebhookWorkerConfig struct {
	PollInterval time.Duration
	BatchSize    int
	MaxRetries   int
}

var DefaultWebhookWorkerConfig = BgWebhookWorkerConfig{
	PollInterval: 5 * time.Second,
	BatchSize:    50,
	MaxRetries:   3,
}

type BgWebhookWorker struct {
	config BgWebhookWorkerConfig
	stopCh chan struct{}
	client *http.Client
}

func NewBgWebhookWorker(cfg BgWebhookWorkerConfig) *BgWebhookWorker {
	return &BgWebhookWorker{
		config: cfg,
		stopCh: make(chan struct{}),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (w *BgWebhookWorker) stopChIfAny() <-chan struct{} {
	return w.stopCh
}

func (w *BgWebhookWorker) Start() {
	go func() {
		ticker := time.NewTicker(w.config.PollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-w.stopChIfAny():
				return
			case <-ticker.C:
				w.ScanAndDispatch()
			}
		}
	}()
}

func (w *BgWebhookWorker) Stop() {
	close(w.stopCh)
}

func (w *BgWebhookWorker) ScanAndDispatch() {
	now := time.Now().Unix()
	events, err := model.GetPendingWebhookEvents(now, w.config.BatchSize)
	if err != nil {
		common.SysError(fmt.Sprintf("webhook_worker: failed to get pending events: %v", err))
		return
	}

	for _, event := range events {
		w.dispatch(event)
	}
}

func (w *BgWebhookWorker) dispatch(event model.BgWebhookEvent) {
	// 1. Lock the event as delivering
	event.DeliveryStatus = model.WebhookStatusDelivering
	if err := model.DB.Save(&event).Error; err != nil {
		return // Someone else picked it up or DB error
	}

	// 2. Resolve Webhook URL (First check BgResponse, then BgSession fallback if needed)
	var webhookURL string
	if event.ResponseID != "" {
		if resp, err := model.GetBgResponseByResponseID(event.ResponseID); err == nil && resp != nil {
			webhookURL = resp.WebhookURL
		}
		if webhookURL == "" {
			if sess, err := model.GetBgSessionBySessionID(event.ResponseID); err == nil && sess != nil {
				webhookURL = sess.WebhookURL
			}
		}
	}

	if webhookURL == "" {
		// Cannot deliver without URL. Mark dead.
		event.DeliveryStatus = model.WebhookStatusDead
		_ = model.DB.Save(&event)
		common.SysError(fmt.Sprintf("webhook_worker: event %s has no URL to dispatch to", event.EventID))
		return
	}

	// 3. Dispatch HTTP Post
	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewBuffer([]byte(event.PayloadJSON)))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
		// Phase 5: HMAC signature would be injected here
		var resp *http.Response
		resp, err = w.client.Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				event.DeliveryStatus = model.WebhookStatusDelivered
				_ = model.DB.Save(&event)
				common.SysLog(fmt.Sprintf("webhook_worker: delivered event %s to %s", event.EventID, webhookURL))
				return
			}
			err = fmt.Errorf("status code %d", resp.StatusCode)
		}
	}

	// 4. Handle Failure & Retries
	common.SysError(fmt.Sprintf("webhook_worker: delivery failed for event %s: %v", event.EventID, err))
	
	event.RetryCount++
	if event.RetryCount > w.config.MaxRetries {
		event.DeliveryStatus = model.WebhookStatusDead
	} else {
		event.DeliveryStatus = model.WebhookStatusRetrying
		// Exponential backoff: 30s, 60s, 120s...
		backoff := 15 * (1 << event.RetryCount)
		event.NextRetryAt = time.Now().Unix() + int64(backoff)
	}
	
	_ = model.DB.Save(&event)
}
