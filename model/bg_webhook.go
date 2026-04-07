package model

const (
	WebhookStatusPending    = "pending"
	WebhookStatusDelivering = "delivering"
	WebhookStatusDelivered  = "delivered"
	WebhookStatusRetrying   = "retrying"
	WebhookStatusDead       = "dead"
)

// BgWebhookEvent represents the bg_webhook_events table.
type BgWebhookEvent struct {
	ID             int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	EventID        string `json:"event_id" gorm:"type:varchar(64);uniqueIndex;not null"`
	ResponseID     string `json:"response_id" gorm:"type:varchar(64);index"`
	OrgID          int    `json:"org_id" gorm:"index;not null;default:0"`
	EventType      string `json:"event_type" gorm:"type:varchar(50);not null"`
	PayloadJSON    string `json:"payload_json" gorm:"type:text"`
	DeliveryStatus string `json:"delivery_status" gorm:"type:varchar(20);not null;default:'pending'"`
	RetryCount     int    `json:"retry_count" gorm:"default:0"`
	NextRetryAt    int64  `json:"next_retry_at" gorm:"default:0;index"`
	Signature      string `json:"signature" gorm:"type:varchar(255)"` // Reserved for HMAC Phase 5
	CreatedAt      int64  `json:"created_at" gorm:"autoCreateTime"`
}

func (BgWebhookEvent) TableName() string {
	return "bg_webhook_events"
}

func (e *BgWebhookEvent) Insert() error {
	return DB.Create(e).Error
}

// GetPendingWebhookEvents returns events that need delivery.
func GetPendingWebhookEvents(now int64, limit int) ([]BgWebhookEvent, error) {
	var events []BgWebhookEvent
	err := DB.Where("delivery_status IN ? AND (next_retry_at = 0 OR next_retry_at <= ?)",
		[]string{WebhookStatusPending, WebhookStatusRetrying}, now).
		Order("id ASC").
		Limit(limit).
		Find(&events).Error
	return events, err
}
