package model

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// BgRoutingPolicy represents a routing strategy for capabilities.
type BgRoutingPolicy struct {
	ID                int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	Scope             string `gorm:"type:varchar(20);not null;index:idx_rt_pol_scope" json:"scope"`
	ScopeID           int    `gorm:"not null;default:0;index:idx_rt_pol_scope" json:"scope_id"`
	CapabilityPattern string `gorm:"type:varchar(191);not null;index:idx_rt_pol_pattern" json:"capability_pattern"`
	Strategy          string `gorm:"type:varchar(30);not null;default:'weighted'" json:"strategy"` // weighted | fixed | primary_backup | byo_first
	RulesJSON         string `gorm:"type:text" json:"rules_json"`
	Priority          int    `gorm:"not null;default:0" json:"priority"`
	Description       string `gorm:"type:varchar(500)" json:"description"`
	Status            string `gorm:"type:varchar(20);not null;default:'active'" json:"status"`
	CreatedAt         int64  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         int64  `gorm:"autoUpdateTime" json:"updated_at"`
}

// Rules structs for each strategy
type FixedRules struct {
	AdapterName string `json:"adapter_name"`
}

type WeightedRules struct {
	Weights map[string]int `json:"weights"`
}

type PrimaryBackupRules struct {
	Primary  string   `json:"primary"`
	Fallback []string `json:"fallback"`
}

type BYOFirstRules struct {
	BYOAdapterPattern string `json:"byo_adapter_pattern"`
}

// Validate checks structural constraints and parses RulesJSON logically.
func (p *BgRoutingPolicy) Validate() error {
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

	validStrategies := map[string]bool{"weighted": true, "fixed": true, "primary_backup": true, "byo_first": true}
	if !validStrategies[p.Strategy] {
		return fmt.Errorf("invalid strategy: %s", p.Strategy)
	}

	switch p.Strategy {
	case "fixed":
		var r FixedRules
		if err := common.Unmarshal([]byte(p.RulesJSON), &r); err != nil {
			return fmt.Errorf("invalid rules_json for fixed strategy: %w", err)
		}
		if r.AdapterName == "" {
			return fmt.Errorf("fixed strategy requires 'adapter_name'")
		}
	case "weighted":
		var r WeightedRules
		if err := common.Unmarshal([]byte(p.RulesJSON), &r); err != nil {
			return fmt.Errorf("invalid rules_json for weighted strategy: %w", err)
		}
		if len(r.Weights) == 0 {
			return fmt.Errorf("weighted strategy requires 'weights' map")
		}
		for k, v := range r.Weights {
			if v <= 0 {
				return fmt.Errorf("weight for adapter '%s' must be > 0, got %d", k, v)
			}
		}
	case "primary_backup":
		var r PrimaryBackupRules
		if err := common.Unmarshal([]byte(p.RulesJSON), &r); err != nil {
			return fmt.Errorf("invalid rules_json for primary_backup strategy: %w", err)
		}
		if r.Primary == "" {
			return fmt.Errorf("primary_backup strategy requires 'primary'")
		}
	case "byo_first":
		var r BYOFirstRules
		if err := common.Unmarshal([]byte(p.RulesJSON), &r); err != nil {
			return fmt.Errorf("invalid rules_json for byo_first strategy: %w", err)
		}
		if r.BYOAdapterPattern == "" {
			return fmt.Errorf("byo_first strategy requires 'byo_adapter_pattern'")
		}
	}

	return nil
}

// CreateBgRoutingPolicy inserts a new policy into the database.
func CreateBgRoutingPolicy(policy *BgRoutingPolicy) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	return DB.Create(policy).Error
}

// GetActiveBgRoutingPolicies returns all active policies. Used by the cache refresher.
func GetActiveBgRoutingPolicies() ([]BgRoutingPolicy, error) {
	var policies []BgRoutingPolicy
	err := DB.Where("status = ?", "active").Find(&policies).Error
	return policies, err
}

// UpdateBgRoutingPolicy updates an existing policy.
func UpdateBgRoutingPolicy(policy *BgRoutingPolicy) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	return DB.Model(policy).Select(
		"scope", "scope_id", "capability_pattern", "strategy",
		"rules_json", "priority", "description", "status",
	).Updates(policy).Error
}

// DeleteBgRoutingPolicy removes a policy by ID.
func DeleteBgRoutingPolicy(id int64) error {
	result := DB.Delete(&BgRoutingPolicy{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("not found")
	}
	return nil
}

// ListBgRoutingPolicies returns a paginated list of policies for a given scope.
func ListBgRoutingPolicies(scope string, scopeID int, offset, limit int) ([]BgRoutingPolicy, int64, error) {
	var policies []BgRoutingPolicy
	var total int64

	query := DB.Model(&BgRoutingPolicy{})
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
