package service

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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

	// Cache webhook secrets per OrgID to avoid N+1 DB queries within a batch.
	// NOTE: OrgID maps to user ID in the current single-user-per-org identity model.
	// When real organization tables exist this should query org settings instead.
	secretCache := make(map[int]string)

	for _, event := range events {
		secret := resolveWebhookSecret(event.OrgID, secretCache)
		w.dispatch(event, secret)
	}
}

// resolveWebhookSecret looks up the webhook secret for a given orgID (currently == userID),
// caching the result in secretCache to prevent duplicate DB reads within one ScanAndDispatch cycle.
func resolveWebhookSecret(orgID int, cache map[int]string) string {
	if s, ok := cache[orgID]; ok {
		return s
	}
	// OrgID == UserID in the current identity model (single-user-per-org).
	user, err := model.GetUserById(orgID, false)
	if err != nil || user == nil {
		cache[orgID] = ""
		return ""
	}
	secret := user.GetSetting().WebhookSecret
	if secret == "" {
		common.SysLog(fmt.Sprintf("webhook_worker: org %d has no webhook_secret configured — sending unsigned", orgID))
	}
	cache[orgID] = secret
	return secret
}

func (w *BgWebhookWorker) dispatch(event model.BgWebhookEvent, webhookSecret string) {
	// 1. Lock the event as delivering
	event.DeliveryStatus = model.WebhookStatusDelivering
	if err := model.DB.Save(&event).Error; err != nil {
		return // Someone else picked it up or DB error
	}

	// 2. Resolve Webhook URL (BgResponse first, BgSession fallback)
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
		event.DeliveryStatus = model.WebhookStatusDead
		_ = model.DB.Save(&event)
		common.SysError(fmt.Sprintf("webhook_worker: event %s has no URL to dispatch to", event.EventID))
		return
	}

	// 3. Build and sign the HTTP request
	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewBuffer([]byte(event.PayloadJSON)))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")

		// Attach HMAC signature if a secret is configured for this org
		if webhookSecret != "" {
			mac := hmac.New(sha256.New, []byte(webhookSecret))
			mac.Write([]byte(event.PayloadJSON))
			req.Header.Set("X-BaseGate-Signature256", hex.EncodeToString(mac.Sum(nil)))
		}

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

	// 4. Handle failure and schedule retry
	common.SysError(fmt.Sprintf("webhook_worker: delivery failed for event %s: %v", event.EventID, err))

	event.RetryCount++
	if event.RetryCount > w.config.MaxRetries {
		event.DeliveryStatus = model.WebhookStatusDead
	} else {
		event.DeliveryStatus = model.WebhookStatusRetrying
		// Exponential backoff: 30s, 60s, 120s…
		backoff := 15 * (1 << event.RetryCount)
		event.NextRetryAt = time.Now().Unix() + int64(backoff)
	}

	_ = model.DB.Save(&event)
}
