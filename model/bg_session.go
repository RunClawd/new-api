package model

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

// GetIdleSessions returns sessions that have been idle longer than their timeout.
func GetIdleSessions(now int64, limit int) ([]BgSession, error) {
	var sessions []BgSession
	err := DB.Where("status = ? AND (last_action_at + idle_timeout_sec) < ?",
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

// BgSessionAction represents the bg_session_actions table.
type BgSessionAction struct {
	ID          int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	ActionID    string `json:"action_id" gorm:"type:varchar(64);uniqueIndex;not null"`
	SessionID   string `json:"session_id" gorm:"type:varchar(64);index;not null"`
	ActionType  string `json:"action_type" gorm:"type:varchar(40);not null"`
	InputJSON   string `json:"input_json" gorm:"type:text"`
	OutputJSON  string `json:"output_json" gorm:"type:text"`
	Status      string `json:"status" gorm:"type:varchar(20);not null;default:'running'"`
	UsageJSON   string `json:"usage_json" gorm:"type:text"`
	StartedAt   int64  `json:"started_at" gorm:"default:0"`
	CompletedAt int64  `json:"completed_at" gorm:"default:0"`
	ErrorJSON   string `json:"error_json" gorm:"type:text"`
}

func (BgSessionAction) TableName() string {
	return "bg_session_actions"
}

func (a *BgSessionAction) Insert() error {
	return DB.Create(a).Error
}
