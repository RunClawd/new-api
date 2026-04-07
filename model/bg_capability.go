package model

type BgCapability struct {
	ID             int64  `gorm:"primaryKey"`
	CapabilityName string `json:"capability_name" gorm:"type:varchar(191);uniqueIndex;not null"`
	Domain         string `json:"domain" gorm:"type:varchar(50)"`
	Action         string `json:"action" gorm:"type:varchar(50)"`
	Tier           string `json:"tier" gorm:"type:varchar(50)"`
	SupportedModes string `json:"supported_modes" gorm:"type:varchar(100)"` // "sync,stream"
	BillableUnit   string `json:"billable_unit" gorm:"type:varchar(50)"`  // "token", "minute"
	SupportsCancel bool   `json:"supports_cancel" gorm:"default:false"`
	Description    string `json:"description" gorm:"type:text"`
	Status         string `json:"status" gorm:"type:varchar(20);default:'active'"`
	CreatedAt      int64  `json:"created_at" gorm:"autoCreateTime"`
}

// GetActiveBgCapabilities returns a list of active capabilities matching the basegate capabilities.
func GetActiveBgCapabilities() ([]*BgCapability, error) {
	var caps []*BgCapability
	err := DB.Where("status = ?", "active").Find(&caps).Error
	return caps, err
}
