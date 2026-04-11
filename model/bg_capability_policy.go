package model

import (
	"fmt"
	"strings"
)

// BgCapabilityPolicy represents an authorization rule for accessing capabilities.
type BgCapabilityPolicy struct {
	ID                int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	Scope             string `gorm:"type:varchar(20);not null;index:idx_cap_pol_scope" json:"scope"`           // platform | org | project | key
	ScopeID           int    `gorm:"not null;default:0;index:idx_cap_pol_scope" json:"scope_id"`               // 0 for platform scope
	CapabilityPattern string `gorm:"type:varchar(191);not null;index:idx_cap_pol_pattern" json:"capability_pattern"` // "bg.llm.*" or "bg.video.generate.standard"
	Action            string `gorm:"type:varchar(10);not null;default:'allow'" json:"action"`                    // allow | deny
	Enforced          bool   `gorm:"not null;default:false" json:"enforced"`                                     // true = cannot be overridden by lower scope
	MaxConcurrency    int    `gorm:"not null;default:0" json:"max_concurrency"`                                  // reserved, validate enforces 0
	Priority          int    `gorm:"not null;default:0" json:"priority"`                                         // higher = evaluated first within same scope
	Description       string `gorm:"type:varchar(500)" json:"description"`
	Status            string `gorm:"type:varchar(20);not null;default:'active'" json:"status"`                   // active | disabled
	CreatedAt         int64  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         int64  `gorm:"autoUpdateTime" json:"updated_at"`
}

// Validate checks if the policy definition is structurally correct.
func (p *BgCapabilityPolicy) Validate() error {
	validScopes := map[string]bool{"platform": true, "org": true, "project": true, "key": true}
	if !validScopes[p.Scope] {
		return fmt.Errorf("invalid scope: %s", p.Scope)
	}
	if p.Scope == "platform" && p.ScopeID != 0 {
		return fmt.Errorf("platform scope must have scope_id=0")
	}
	if !strings.HasPrefix(p.CapabilityPattern, "bg.") {
		return fmt.Errorf("capability_pattern must start with 'bg.'")
	}
	
	// Wildcard '*' must be the last segment
	parts := strings.Split(p.CapabilityPattern, ".")
	for i, part := range parts {
		if part == "*" && i != len(parts)-1 {
			return fmt.Errorf("wildcard '*' must be the last segment in capability_pattern")
		}
	}
	
	if p.Status == "" {
		p.Status = "active"
	}
	if p.Status != "active" && p.Status != "disabled" {
		return fmt.Errorf("status must be 'active' or 'disabled'")
	}

	if p.Action != "allow" && p.Action != "deny" {
		return fmt.Errorf("action must be 'allow' or 'deny'")
	}
	if p.Enforced && p.Action != "deny" {
		return fmt.Errorf("enforced=true is only allowed with action='deny'")
	}
	if p.Enforced && p.Scope != "platform" {
		return fmt.Errorf("enforced=true is only allowed on platform scope")
	}
	if p.MaxConcurrency != 0 {
		return fmt.Errorf("max_concurrency is reserved for future use and must be 0")
	}
	return nil
}

// CreateBgCapabilityPolicy inserts a new policy into the database.
func CreateBgCapabilityPolicy(policy *BgCapabilityPolicy) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	return DB.Create(policy).Error
}

// GetActiveBgCapabilityPolicies returns all active policies. Used by the cache refresher.
func GetActiveBgCapabilityPolicies() ([]BgCapabilityPolicy, error) {
	var policies []BgCapabilityPolicy
	err := DB.Where("status = ?", "active").Find(&policies).Error
	return policies, err
}

// UpdateBgCapabilityPolicy updates an existing policy.
func UpdateBgCapabilityPolicy(policy *BgCapabilityPolicy) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	result := DB.Model(policy).Select(
		"scope", "scope_id", "capability_pattern", "action",
		"enforced", "max_concurrency", "priority",
		"description", "status",
	).Updates(policy)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("not found")
	}
	return nil
}

// DeleteBgCapabilityPolicy removes a policy by ID.
func DeleteBgCapabilityPolicy(id int64) error {
	result := DB.Delete(&BgCapabilityPolicy{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("not found")
	}
	return nil
}

// ListBgCapabilityPolicies returns a paginated list of policies for a given scope.
func ListBgCapabilityPolicies(scope string, scopeID int, offset, limit int) ([]BgCapabilityPolicy, int64, error) {
	var policies []BgCapabilityPolicy
	var total int64
	
	query := DB.Model(&BgCapabilityPolicy{})
	if scope != "" {
		query = query.Where("scope = ?", scope)
		if scopeID != 0 {
			query = query.Where("scope_id = ?", scopeID)
		}
	}
	
	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}
	
	err = query.Order("id desc").Offset(offset).Limit(limit).Find(&policies).Error
	return policies, total, err
}
