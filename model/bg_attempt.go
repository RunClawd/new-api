package model

// BgAttemptStatus defines the valid statuses for a Response Attempt.
type BgAttemptStatus string

const (
	BgAttemptStatusDispatching BgAttemptStatus = "dispatching"
	BgAttemptStatusAccepted    BgAttemptStatus = "accepted"
	BgAttemptStatusRunning     BgAttemptStatus = "running"
	BgAttemptStatusSucceeded   BgAttemptStatus = "succeeded"
	BgAttemptStatusFailed      BgAttemptStatus = "failed"
	BgAttemptStatusCanceled    BgAttemptStatus = "canceled"
	BgAttemptStatusAbandoned   BgAttemptStatus = "abandoned"
	BgAttemptStatusUnknown     BgAttemptStatus = "unknown"
)

func (s BgAttemptStatus) IsTerminal() bool {
	switch s {
	case BgAttemptStatusSucceeded, BgAttemptStatusFailed,
		BgAttemptStatusCanceled, BgAttemptStatusAbandoned:
		return true
	}
	return false
}

// BgResponseAttempt represents the bg_response_attempts table.
type BgResponseAttempt struct {
	ID                int64           `json:"id" gorm:"primaryKey;autoIncrement"`
	AttemptID         string          `json:"attempt_id" gorm:"type:varchar(64);uniqueIndex;not null"`
	ResponseID        string          `json:"response_id" gorm:"type:varchar(64);index;not null"`
	AttemptNo         int             `json:"attempt_no" gorm:"not null;default:1"`
	AdapterName       string          `json:"adapter_name" gorm:"type:varchar(100)"`
	ProviderRequestID string          `json:"provider_request_id" gorm:"type:varchar(191)"`
	Status            BgAttemptStatus `json:"status" gorm:"type:varchar(20);index;not null;default:'dispatching'"`
	StatusVersion     int             `json:"status_version" gorm:"not null;default:1"`
	ErrorJSON         string          `json:"error_json" gorm:"type:text"`
	StartedAt         int64           `json:"started_at" gorm:"default:0"`
	AcceptedAt        int64           `json:"accepted_at" gorm:"default:0"`
	CompletedAt       int64           `json:"completed_at" gorm:"default:0"`
	PollAfterAt       int64           `json:"poll_after_at" gorm:"default:0;index"`
	PollCount         int             `json:"poll_count" gorm:"default:0"`
	LastPollAt        int64           `json:"last_poll_at" gorm:"default:0"`
}

func (BgResponseAttempt) TableName() string {
	return "bg_response_attempts"
}

func (a *BgResponseAttempt) Insert() error {
	return DB.Create(a).Error
}

func GetBgAttemptsByResponseID(responseID string) ([]BgResponseAttempt, error) {
	var attempts []BgResponseAttempt
	err := DB.Where("response_id = ?", responseID).Order("attempt_no ASC").Find(&attempts).Error
	return attempts, err
}

// GetBgAttemptByAttemptID finds a single attempt by its public attempt_id.
func GetBgAttemptByAttemptID(attemptID string) (*BgResponseAttempt, error) {
	var attempt BgResponseAttempt
	err := DB.Where("attempt_id = ?", attemptID).First(&attempt).Error
	if err != nil {
		return nil, err
	}
	return &attempt, nil
}

// GetPollableAttempts returns attempts that need polling (poll_after_at <= now, non-terminal).
func GetPollableAttempts(now int64, limit int) ([]BgResponseAttempt, error) {
	var attempts []BgResponseAttempt
	err := DB.Where("poll_after_at > 0 AND poll_after_at <= ? AND status NOT IN ?",
		now, []BgAttemptStatus{
			BgAttemptStatusSucceeded, BgAttemptStatusFailed,
			BgAttemptStatusCanceled, BgAttemptStatusAbandoned,
		}).
		Order("poll_after_at ASC").
		Limit(limit).
		Find(&attempts).Error
	return attempts, err
}

// CASUpdateStatus atomically updates attempt status with optimistic locking.
func (a *BgResponseAttempt) CASUpdateStatus(expectedStatus BgAttemptStatus, expectedVersion int) (bool, error) {
	result := DB.Model(&BgResponseAttempt{}).
		Where("id = ? AND status = ? AND status_version = ?", a.ID, expectedStatus, expectedVersion).
		Updates(map[string]interface{}{
			"status":              a.Status,
			"status_version":      expectedVersion + 1,
			"error_json":         a.ErrorJSON,
			"completed_at":       a.CompletedAt,
			"poll_after_at":      a.PollAfterAt,
			"poll_count":         a.PollCount,
			"last_poll_at":       a.LastPollAt,
			"provider_request_id": a.ProviderRequestID,
		})

	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 0 {
		return false, nil
	}
	a.StatusVersion = expectedVersion + 1
	return true, nil
}
