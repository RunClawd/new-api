package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupWebhookWorkerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	model.DB = db
	model.LOG_DB = db

	if err := db.AutoMigrate(
		&model.BgResponse{},
		&model.BgWebhookEvent{},
	); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestBgWebhookWorker_Delivery(t *testing.T) {
	setupWebhookWorkerTestDB(t)

	// Mock Sink Server
	var receivedPayload map[string]interface{}
	sinkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(bodyBytes, &receivedPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer sinkServer.Close()

	// Setup Response with WebhookURL
	resp := &model.BgResponse{
		ResponseID: "resp_wh_1",
		Model:      "test",
		WebhookURL: sinkServer.URL,
	}
	require.NoError(t, resp.Insert())

	// Enqueue an event
	payload := map[string]interface{}{"status": "succeeded"}
	err := EnqueueWebhookEvent("resp_wh_1", 1, "response.completed", payload)
	require.NoError(t, err)

	worker := NewBgWebhookWorker(BgWebhookWorkerConfig{
		PollInterval: 1 * time.Second,
		BatchSize:    10,
		MaxRetries:   3,
	})

	// Perform a single scan pass
	worker.ScanAndDispatch()

	// Verify Delivery
	assert.NotNil(t, receivedPayload)
	assert.Equal(t, "succeeded", receivedPayload["status"])

	// Check DB state
	events, _ := model.GetPendingWebhookEvents(time.Now().Unix(), 10)
	assert.Empty(t, events, "delivered events should not be pending")
}

func TestBgWebhookWorker_RetryAndDead(t *testing.T) {
	setupWebhookWorkerTestDB(t)

	failCount := 0
	sinkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		failCount++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer sinkServer.Close()

	resp := &model.BgResponse{
		ResponseID: "resp_wh_fail",
		Model:      "test",
		WebhookURL: sinkServer.URL,
	}
	require.NoError(t, resp.Insert())

	err := EnqueueWebhookEvent("resp_wh_fail", 1, "response.failed", nil)
	require.NoError(t, err)

	worker := NewBgWebhookWorker(BgWebhookWorkerConfig{
		PollInterval: 1 * time.Second,
		BatchSize:    10,
		MaxRetries:   2, // Will fail on 3rd attempt
	})

	// Pass 1 -> Retrying
	worker.ScanAndDispatch()
	assert.Equal(t, 1, failCount)

	// Since we use backoff, it won't pick it up immediately
	// Fast forward the next_retry_at using raw SQL
	model.DB.Exec("UPDATE bg_webhook_events SET next_retry_at = 0")

	// Pass 2 -> Retrying
	worker.ScanAndDispatch()
	assert.Equal(t, 2, failCount)
	model.DB.Exec("UPDATE bg_webhook_events SET next_retry_at = 0")

	// Pass 3 -> Dead (MaxRetries is 2, so 3 attempts total: 0, 1, 2)
	worker.ScanAndDispatch()
	assert.Equal(t, 3, failCount)

	var ev model.BgWebhookEvent
	model.DB.First(&ev)
	assert.Equal(t, model.WebhookStatusDead, ev.DeliveryStatus)
	assert.Equal(t, 3, ev.RetryCount)
}
