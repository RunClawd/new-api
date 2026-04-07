package model

// BgUsageStatus defines usage record lifecycle states.
type BgUsageStatus string

const (
	BgUsageStatusPending    BgUsageStatus = "pending"
	BgUsageStatusCollecting BgUsageStatus = "collecting"
	BgUsageStatusNormalized BgUsageStatus = "normalized"
	BgUsageStatusFinalized  BgUsageStatus = "finalized"
	BgUsageStatusVoided     BgUsageStatus = "voided"
)

// BgUsageRecord represents the bg_usage_records table.
type BgUsageRecord struct {
	ID                 int64         `json:"id" gorm:"primaryKey;autoIncrement"`
	UsageID            string        `json:"usage_id" gorm:"type:varchar(64);uniqueIndex;not null"`
	ResponseID         string        `json:"response_id" gorm:"type:varchar(64);index;not null"`
	OrgID              int           `json:"org_id" gorm:"index;index:idx_usage_aggr,priority:1;not null;default:0"`
	ProjectID          int           `json:"project_id" gorm:"not null;default:0"`
	Provider           string        `json:"provider" gorm:"type:varchar(50)"`
	Model              string        `json:"model" gorm:"type:varchar(191);index:idx_usage_aggr,priority:2"`
	BillableUnits      float64       `json:"billable_units" gorm:"type:decimal(20,6);default:0"`
	BillableUnit       string        `json:"billable_unit" gorm:"type:varchar(20)"`
	InputUnits         float64       `json:"input_units" gorm:"type:decimal(20,6);default:0"`
	OutputUnits        float64       `json:"output_units" gorm:"type:decimal(20,6);default:0"`
	RawUsageJSON       string        `json:"raw_usage_json" gorm:"type:text"`
	CanonicalUsageJSON string        `json:"canonical_usage_json" gorm:"type:text"`
	Status             BgUsageStatus `json:"status" gorm:"type:varchar(20);not null;default:'pending'"`
	Version            int           `json:"version" gorm:"not null;default:1"`
	CreatedAt          int64         `json:"created_at" gorm:"autoCreateTime;index:idx_usage_aggr,priority:3"`
}

func (BgUsageRecord) TableName() string {
	return "bg_usage_records"
}

func (u *BgUsageRecord) Insert() error {
	return DB.Create(u).Error
}

// BgBillingStatus defines billing record lifecycle states.
type BgBillingStatus string

const (
	BgBillingStatusPending  BgBillingStatus = "pending"
	BgBillingStatusEstimated BgBillingStatus = "estimated"
	BgBillingStatusPosted   BgBillingStatus = "posted"
	BgBillingStatusSettled  BgBillingStatus = "settled"
	BgBillingStatusRefunded BgBillingStatus = "refunded"
	BgBillingStatusVoided   BgBillingStatus = "voided"
)

// BgBillingRecord represents the bg_billing_records table.
type BgBillingRecord struct {
	ID                  int64           `json:"id" gorm:"primaryKey;autoIncrement"`
	BillingID           string          `json:"billing_id" gorm:"type:varchar(64);uniqueIndex;not null"`
	ResponseID          string          `json:"response_id" gorm:"type:varchar(64);index;not null"`
	OrgID               int             `json:"org_id" gorm:"index;not null;default:0"`
	ProjectID           int             `json:"project_id" gorm:"not null;default:0"`
	BillingMode         string          `json:"billing_mode" gorm:"type:varchar(10);default:'hosted'"`
	BillableUnit        string          `json:"billable_unit" gorm:"type:varchar(20)"`
	Quantity            float64         `json:"quantity" gorm:"type:decimal(20,6);default:0"`
	UnitPrice           float64         `json:"unit_price" gorm:"type:decimal(20,10);default:0"`
	Amount              float64         `json:"amount" gorm:"type:decimal(20,6);default:0"`
	PricingSnapshotJSON string          `json:"pricing_snapshot_json" gorm:"type:text"`
	BillableUnits       float64         `json:"billable_units" gorm:"type:decimal(20,6);default:0"`
	TotalAmount         float64         `json:"total_amount" gorm:"type:decimal(20,6);default:0"`
	ProviderCost        float64         `json:"provider_cost" gorm:"type:decimal(20,6);default:0"`
	PlatformMargin      float64         `json:"platform_margin" gorm:"type:decimal(20,6);default:0"`
	Currency            string          `json:"currency" gorm:"type:varchar(10);default:'usd'"`
	Status              BgBillingStatus `json:"status" gorm:"type:varchar(20);not null;default:'pending'"`
	CreatedAt           int64           `json:"created_at" gorm:"autoCreateTime"`
}

func (BgBillingRecord) TableName() string {
	return "bg_billing_records"
}

func (b *BgBillingRecord) Insert() error {
	return DB.Create(b).Error
}

// BgLedgerEntry represents the bg_ledger_entries table.
type BgLedgerEntry struct {
	ID            int64   `json:"id" gorm:"primaryKey;autoIncrement"`
	LedgerEntryID string  `json:"ledger_entry_id" gorm:"type:varchar(64);uniqueIndex;not null"`
	OrgID         int     `json:"org_id" gorm:"index;not null;default:0"`
	ResponseID    string  `json:"response_id" gorm:"type:varchar(64);index"`
	BillingID     string  `json:"billing_id" gorm:"type:varchar(64);index"`
	EntryType     string  `json:"entry_type" gorm:"type:varchar(30);not null"`
	Direction     string  `json:"direction" gorm:"type:varchar(10);not null;default:'debit'"`
	Amount        float64 `json:"amount" gorm:"type:decimal(20,6);not null"`
	Currency      string  `json:"currency" gorm:"type:varchar(10);default:'usd'"`
	BalanceAfter  float64 `json:"balance_after" gorm:"type:decimal(20,6);default:0"`
	Status        string  `json:"status" gorm:"type:varchar(20);default:'committed'"`
	CreatedAt     int64   `json:"created_at" gorm:"autoCreateTime"`
}

func (BgLedgerEntry) TableName() string {
	return "bg_ledger_entries"
}

func (l *BgLedgerEntry) Insert() error {
	return DB.Create(l).Error
}
