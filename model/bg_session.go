package model

import (
	"fmt"
	"time"
)

// BgSessionStatus defines session lifecycle states.
type BgSessionStatus string

const (
	BgSessionStatusCreating  BgSessionStatus = "creating"
	BgSessionStatusActive    BgSessionStatus = "active"
	BgSessionStatusIdle      BgSessionStatus = "idle"
	BgSessionStatusSuspended BgSessionStatus = "suspended"
	BgSessionStatusClosed    BgSessionStatus = "closed"
	BgSessionStatusExpired   BgSessionStatus = "expired"
	BgSessionStatusFailed    BgSessionStatus = "failed"
)

func (s BgSessionStatus) IsTerminal() bool {
	switch s {
	case BgSessionStatusClosed, BgSessionStatusExpired, BgSessionStatusFailed:
		return true
	}
	return false
}

// SessionValidTransitions enforces the strict state machine for session lifecycle.
var SessionValidTransitions = map[BgSessionStatus][]BgSessionStatus{
	BgSessionStatusCreating: {BgSessionStatusActive, BgSessionStatusFailed},
	BgSessionStatusActive:   {BgSessionStatusIdle, BgSessionStatusClosed, BgSessionStatusExpired, BgSessionStatusFailed},
	BgSessionStatusIdle:     {BgSessionStatusActive, BgSessionStatusClosed, BgSessionStatusExpired},
	// Terminal states have no outgoing transitions
	BgSessionStatusClosed:  {},
	BgSessionStatusExpired: {},
	BgSessionStatusFailed:  {},
}

// IsValidTransition returns true if the transition from current to target is allowed.
func IsValidSessionTransition(current, target BgSessionStatus) bool {
	if current == target {
		return true // No-op
	}
	allowed, ok := SessionValidTransitions[current]
	if !ok {
		return false
	}
	for _, a := range allowed {
		if a == target {
			return true
		}
	}
	return false
}

// BgSession represents the bg_sessions table.
type BgSession struct {
	ID                int64           `json:"id" gorm:"primaryKey;autoIncrement"`
	SessionID         string          `json:"session_id" gorm:"type:varchar(64);uniqueIndex;not null"`
	ResponseID        string          `json:"response_id" gorm:"type:varchar(64);index;not null"`
	OrgID             int             `json:"org_id" gorm:"index;not null;default:0"`
	ProjectID         int             `json:"project_id" gorm:"not null;default:0"`
	ApiKeyID          int             `json:"api_key_id" gorm:"not null;default:0"`
	Model             string          `json:"model" gorm:"type:varchar(191);index;not null"`
	Status            BgSessionStatus `json:"status" gorm:"type:varchar(20);index;not null;default:'creating'"`
	AdapterName       string          `json:"adapter_name" gorm:"type:varchar(100)"`
	ProviderSessionID string          `json:"provider_session_id" gorm:"type:varchar(191)"`
	UsageJSON         string          `json:"usage_json" gorm:"type:text"`
	ConfigJSON        string          `json:"config_json" gorm:"type:text"`
	IdleTimeoutSec    int             `json:"idle_timeout_sec" gorm:"default:300"`
	MaxDurationSec    int             `json:"max_duration_sec" gorm:"default:3600"`
	ExpiresAt         int64           `json:"expires_at" gorm:"default:0"`
	LastActionAt      int64           `json:"last_action_at" gorm:"default:0"`
	ClosedAt          int64           `json:"closed_at" gorm:"default:0"`
	ActionLockVersion int             `json:"action_lock_version" gorm:"not null;default:1"`
	StatusVersion     int             `json:"status_version" gorm:"not null;default:1"`
	CreatedAt         int64           `json:"created_at" gorm:"autoCreateTime"`
}

func (BgSession) TableName() string {
	return "bg_sessions"
}

func (s *BgSession) Insert() error {
	return DB.Create(s).Error
}

func GetBgSessionBySessionID(sessionID string) (*BgSession, error) {
	var sess BgSession
	err := DB.Where("session_id = ?", sessionID).First(&sess).Error
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

// GetIdleSessions returns active sessions that have been idle longer than their timeout.
func GetIdleSessions(now int64, limit int) ([]BgSession, error) {
	var sessions []BgSession
	err := DB.Where("status = ? AND idle_timeout_sec > 0 AND (last_action_at + idle_timeout_sec) <= ?",
		BgSessionStatusActive, now).
		Limit(limit).
		Find(&sessions).Error
	return sessions, err
}

// GetExpiredSessions returns active/idle sessions past their expires_at.
func GetExpiredSessions(now int64, limit int) ([]BgSession, error) {
	var sessions []BgSession
	err := DB.Where("expires_at > 0 AND expires_at <= ? AND status IN ?",
		now, []BgSessionStatus{BgSessionStatusActive, BgSessionStatusIdle, BgSessionStatusCreating}).
		Limit(limit).
		Find(&sessions).Error
	return sessions, err
}

// CASUpdateStatus updates the session status with optimistic concurrency control.
// It also enforces the transition logic.
func (s *BgSession) CASUpdateStatus(expectedStatus BgSessionStatus, expectedVersion int, targetStatus BgSessionStatus) (bool, error) {
	if !IsValidSessionTransition(expectedStatus, targetStatus) {
		return false, fmt.Errorf("invalid session transition: %s -> %s", expectedStatus, targetStatus)
	}

	updates := map[string]interface{}{
		"status":         targetStatus,
		"status_version": expectedVersion + 1,
	}

	if targetStatus.IsTerminal() && s.ClosedAt == 0 {
		s.ClosedAt = time.Now().Unix()
		updates["closed_at"] = s.ClosedAt
	}

	result := DB.Model(&BgSession{}).
		Where("id = ? AND status = ? AND status_version = ?", s.ID, expectedStatus, expectedVersion).
		Updates(updates)

	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 0 {
		return false, nil
	}
	s.Status = targetStatus
	s.StatusVersion = expectedVersion + 1
	return true, nil
}

// AcquireActionLock atomically increments the action lock version to prevent concurrent dispatch.
func (s *BgSession) AcquireActionLock() (bool, error) {
	result := DB.Model(&BgSession{}).
		Where("id = ? AND action_lock_version = ?", s.ID, s.ActionLockVersion).
		Updates(map[string]interface{}{
			"action_lock_version": s.ActionLockVersion + 1,
			"last_action_at":      time.Now().Unix(),
		})

	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 0 {
		return false, nil
	}
	s.ActionLockVersion++
	return true, nil
}

// BgSessionAction represents the bg_session_actions table.
type BgSessionAction struct {
	ID          int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	ActionID    string `json:"action_id" gorm:"type:varchar(64);uniqueIndex;not null"`
	SessionID   string `json:"session_id" gorm:"type:varchar(64);index;not null"`
	ActionType     string `json:"action_type" gorm:"type:varchar(40);not null"`
	IdempotencyKey string `json:"idempotency_key" gorm:"type:varchar(100);index"`
	InputJSON      string `json:"input_json" gorm:"type:text"`
	OutputJSON     string `json:"output_json" gorm:"type:text"`
	Status         string `json:"status" gorm:"type:varchar(20);not null;default:'running'"`
	UsageJSON      string `json:"usage_json" gorm:"type:text"`
	StartedAt      int64  `json:"started_at" gorm:"default:0"`
	CompletedAt    int64  `json:"completed_at" gorm:"default:0"`
	ErrorJSON      string `json:"error_json" gorm:"type:text"`
}

func (BgSessionAction) TableName() string {
	return "bg_session_actions"
}

func (a *BgSessionAction) Insert() error {
	return DB.Create(a).Error
}

func GetBgSessionActionByIdempotencyKey(sessionID, idempotencyKey string) (*BgSessionAction, error) {
	var action BgSessionAction
	err := DB.Where("session_id = ? AND idempotency_key = ?", sessionID, idempotencyKey).First(&action).Error
	if err != nil {
		return nil, err
	}
	return &action, nil
}

func GetBgSessionActionsBySessionID(sessionID string) ([]BgSessionAction, error) {
	var actions []BgSessionAction
	err := DB.Where("session_id = ?", sessionID).Order("started_at ASC").Find(&actions).Error
	return actions, err
}
