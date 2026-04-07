package model

// BgResponseStatus defines the valid statuses for a BaseGate Response.
type BgResponseStatus string

const (
	BgResponseStatusAccepted  BgResponseStatus = "accepted"
	BgResponseStatusQueued    BgResponseStatus = "queued"
	BgResponseStatusRunning   BgResponseStatus = "running"
	BgResponseStatusStreaming BgResponseStatus = "streaming"
	BgResponseStatusSucceeded BgResponseStatus = "succeeded"
	BgResponseStatusFailed    BgResponseStatus = "failed"
	BgResponseStatusCanceled  BgResponseStatus = "canceled"
	BgResponseStatusExpired   BgResponseStatus = "expired"
)

// IsTerminal returns true if the status is a final state.
func (s BgResponseStatus) IsTerminal() bool {
	switch s {
	case BgResponseStatusSucceeded, BgResponseStatusFailed,
		BgResponseStatusCanceled, BgResponseStatusExpired:
		return true
	}
	return false
}

// ValidTransitions defines legal state transitions for a Response.
var bgResponseValidTransitions = map[BgResponseStatus][]BgResponseStatus{
	BgResponseStatusAccepted: {BgResponseStatusQueued, BgResponseStatusFailed},
	BgResponseStatusQueued: {
		BgResponseStatusRunning, BgResponseStatusStreaming,
		BgResponseStatusSucceeded, BgResponseStatusFailed,
		BgResponseStatusCanceled, BgResponseStatusExpired,
	},
	BgResponseStatusRunning: {
		BgResponseStatusSucceeded, BgResponseStatusFailed,
		BgResponseStatusCanceled, BgResponseStatusExpired,
	},
	BgResponseStatusStreaming: {
		BgResponseStatusSucceeded, BgResponseStatusFailed,
		BgResponseStatusCanceled,
	},
}

// CanTransitionTo returns true if the transition from current status to target is valid.
func (s BgResponseStatus) CanTransitionTo(target BgResponseStatus) bool {
	if s.IsTerminal() {
		return false
	}
	allowed, ok := bgResponseValidTransitions[s]
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

// BgResponse represents the bg_responses table.
type BgResponse struct {
	ID              int64            `json:"id" gorm:"primaryKey;autoIncrement"`
	ResponseID      string           `json:"response_id" gorm:"type:varchar(64);uniqueIndex;not null"`
	RequestID       string           `json:"request_id" gorm:"type:varchar(64)"`
	OrgID           int              `json:"org_id" gorm:"index;not null;default:0"`
	ProjectID       int              `json:"project_id" gorm:"index;not null;default:0"`
	ApiKeyID        int              `json:"api_key_id" gorm:"index;not null;default:0"`
	EndUserID       string           `json:"end_user_id" gorm:"type:varchar(64)"`
	Model           string           `json:"model" gorm:"type:varchar(191);index;not null"`
	Status          BgResponseStatus `json:"status" gorm:"type:varchar(20);index;not null;default:'accepted'"`
	StatusVersion   int              `json:"status_version" gorm:"not null;default:1"`
	IdempotencyKey  string           `json:"idempotency_key" gorm:"type:varchar(191);index"`
	ActiveAttemptID int64            `json:"active_attempt_id" gorm:"default:0"`
	BillingMode     string           `json:"billing_mode" gorm:"type:varchar(10);default:'hosted'"`
	InputJSON       string           `json:"input_json" gorm:"type:text"`
	OutputJSON      string           `json:"output_json" gorm:"type:text"`
	ErrorJSON       string           `json:"error_json" gorm:"type:text"`
	CreatedAt       int64            `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt       int64            `json:"updated_at" gorm:"autoUpdateTime"`
	FinalizedAt     int64            `json:"finalized_at" gorm:"default:0"`
	ExpiresAt       int64            `json:"expires_at" gorm:"default:0"`
	BillingStatus        string           `json:"billing_status" gorm:"type:varchar(20);default:'none'"` // none | completed | failed
	WebhookURL           string           `json:"webhook_url" gorm:"type:text"`
	// PricingSnapshotJSON holds the pricing frozen at invocation time (immutable).
	// Used by the state machine to avoid price-drift on long-running async/session responses.
	PricingSnapshotJSON  string           `json:"pricing_snapshot_json" gorm:"type:text"`
	// EstimatedQuota is the pre-authorized quota amount reserved at dispatch time.
	// Used by the state machine to settle (refund/charge difference) at terminal state.
	EstimatedQuota       int              `json:"estimated_quota" gorm:"default:0"`
}

func (BgResponse) TableName() string {
	return "bg_responses"
}

// Insert creates a new BgResponse record.
func (r *BgResponse) Insert() error {
	return DB.Create(r).Error
}

// GetByResponseID finds a response by its public response_id.
func GetBgResponseByResponseID(responseID string) (*BgResponse, error) {
	var resp BgResponse
	err := DB.Where("response_id = ?", responseID).First(&resp).Error
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetByIdempotencyKey finds a response by org_id + idempotency_key.
func GetBgResponseByIdempotencyKey(orgID int, key string) (*BgResponse, error) {
	var resp BgResponse
	err := DB.Where("org_id = ? AND idempotency_key = ?", orgID, key).First(&resp).Error
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// CASUpdateStatus atomically transitions a response from expectedStatus to the
// current in-memory status, using optimistic locking on status + status_version.
// Returns (true, nil) on success, (false, nil) if the CAS fails, and (false, err)
// on database error.
func (r *BgResponse) CASUpdateStatus(expectedStatus BgResponseStatus, expectedVersion int) (bool, error) {
	now := r.UpdatedAt
	if now == 0 {
		now = int64(0) // will be set by GORM autoUpdateTime
	}

	result := DB.Model(&BgResponse{}).
		Where("id = ? AND status = ? AND status_version = ?", r.ID, expectedStatus, expectedVersion).
		Updates(map[string]interface{}{
			"status":           r.Status,
			"status_version":   expectedVersion + 1,
			"output_json":     r.OutputJSON,
			"error_json":      r.ErrorJSON,
			"active_attempt_id": r.ActiveAttemptID,
			"finalized_at":    r.FinalizedAt,
		})

	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 0 {
		return false, nil
	}
	r.StatusVersion = expectedVersion + 1
	return true, nil
}

// ListBgResponsesByOrgID returns a paginated list of responses for a specific tenant.
func ListBgResponsesByOrgID(orgID string, offset int, limit int) ([]*BgResponse, error) {
	var responses []*BgResponse
	err := DB.Where("org_id = ?", orgID).
		Order("created_at desc").
		Offset(offset).
		Limit(limit).
		Find(&responses).Error
	return responses, err
}
