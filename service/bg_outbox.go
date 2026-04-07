package service

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// EnqueueWebhookEvent appends a webhook push to the bg_webhook_events outbox table.
func EnqueueWebhookEvent(responseID string, orgID int, eventType string, payload interface{}) error {
	payloadJSON, err := common.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	event := &model.BgWebhookEvent{
		EventID:        relaycommon.GenerateEventID(),
		ResponseID:     responseID,
		OrgID:          orgID,
		EventType:      eventType,
		PayloadJSON:    string(payloadJSON),
		DeliveryStatus: model.WebhookStatusPending,
		CreatedAt:      time.Now().Unix(),
	}

	if err := event.Insert(); err != nil {
		common.SysError(fmt.Sprintf("outbox: failed to enqueue event %s for response %s: %v", eventType, responseID, err))
		return err
	}

	common.SysLog(fmt.Sprintf("outbox: enqueued %s event for response %s", eventType, responseID))
	return nil
}
