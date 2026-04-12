package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBgRoutingPolicy_Validate(t *testing.T) {
	tests := []struct {
		name    string
		policy  BgRoutingPolicy
		wantErr string
	}{
		{
			name: "valid fixed",
			policy: BgRoutingPolicy{
				Scope:             "org",
				ScopeID:           1,
				CapabilityPattern: "bg.llm.*",
				Strategy:          "fixed",
				RulesJSON:         `{"adapter_name": "ch1"}`,
				Status:            "active",
			},
		},
		{
			name: "invalid fixed missing adapter_name",
			policy: BgRoutingPolicy{
				Scope:             "org",
				ScopeID:           1,
				CapabilityPattern: "bg.llm.*",
				Strategy:          "fixed",
				RulesJSON:         `{}`,
				Status:            "active",
			},
			wantErr: "fixed strategy requires 'adapter_name'",
		},
		{
			name: "valid weighted",
			policy: BgRoutingPolicy{
				Scope:             "org",
				ScopeID:           1,
				CapabilityPattern: "bg.llm.*",
				Strategy:          "weighted",
				RulesJSON:         `{"weights": {"ch1": 10, "ch2": 2}}`,
				Status:            "active",
			},
		},
		{
			name: "invalid weighted zero",
			policy: BgRoutingPolicy{
				Scope:             "org",
				ScopeID:           1,
				CapabilityPattern: "bg.llm.*",
				Strategy:          "weighted",
				RulesJSON:         `{"weights": {"ch1": 0}}`,
				Status:            "active",
			},
			wantErr: "weight for adapter 'ch1' must be > 0",
		},
		{
			name: "valid primary_backup",
			policy: BgRoutingPolicy{
				Scope:             "org",
				ScopeID:           1,
				CapabilityPattern: "bg.llm.*",
				Strategy:          "primary_backup",
				RulesJSON:         `{"primary": "ch1", "fallback": ["ch2"]}`,
				Status:            "active",
			},
		},
		{
			name: "invalid primary_backup missing primary",
			policy: BgRoutingPolicy{
				Scope:             "org",
				ScopeID:           1,
				CapabilityPattern: "bg.llm.*",
				Strategy:          "primary_backup",
				RulesJSON:         `{"fallback": ["ch2"]}`,
				Status:            "active",
			},
			wantErr: "primary_backup strategy requires 'primary'",
		},
		{
			name: "valid byo_first per_request",
			policy: BgRoutingPolicy{
				Scope:             "org",
				ScopeID:           1,
				CapabilityPattern: "bg.llm.*",
				Strategy:          "byo_first",
				RulesJSON:         `{"provider": "my-byo-provider", "fee_config": {"fee_type": "per_request", "fixed_amount": 0.05}}`,
				Status:            "active",
			},
		},
		{
			name: "valid byo_first percentage",
			policy: BgRoutingPolicy{
				Scope:             "org",
				ScopeID:           1,
				CapabilityPattern: "bg.llm.*",
				Strategy:          "byo_first",
				RulesJSON:         `{"provider": "my-byo-provider", "fee_config": {"fee_type": "percentage", "percentage_rate": 0.15}}`,
				Status:            "active",
			},
		},
		{
			name: "invalid byo_first missing fee_type",
			policy: BgRoutingPolicy{
				Scope:             "org",
				ScopeID:           1,
				CapabilityPattern: "bg.llm.*",
				Strategy:          "byo_first",
				RulesJSON:         `{"provider": "my-byo-provider", "fee_config": {"fixed_amount": 0.05}}`,
				Status:            "active",
			},
			wantErr: "fee_type must be 'per_request' or 'percentage'",
		},
		{
			name: "invalid byo_first missing provider",
			policy: BgRoutingPolicy{
				Scope:             "org",
				ScopeID:           1,
				CapabilityPattern: "bg.llm.*",
				Strategy:          "byo_first",
				RulesJSON:         `{"fee_config": {"fee_type": "per_request", "fixed_amount": 0.05}}`,
				Status:            "active",
			},
			wantErr: "provider must be specified for byo_first strategy",
		},
		{
			name: "invalid byo_first missing positive fixed_amount",
			policy: BgRoutingPolicy{
				Scope:             "org",
				ScopeID:           1,
				CapabilityPattern: "bg.llm.*",
				Strategy:          "byo_first",
				RulesJSON:         `{"provider": "my-byo-provider", "fee_config": {"fee_type": "per_request"}}`,
				Status:            "active",
			},
			wantErr: "per_request requires positive fixed_amount",
		},
		{
			name: "invalid byo_first invalid percentage",
			policy: BgRoutingPolicy{
				Scope:             "org",
				ScopeID:           1,
				CapabilityPattern: "bg.llm.*",
				Strategy:          "byo_first",
				RulesJSON:         `{"provider": "my-byo-provider", "fee_config": {"fee_type": "percentage", "percentage_rate": 1.5}}`,
				Status:            "active",
			},
			wantErr: "percentage_rate must be in (0, 1]",
		},
		{
			name: "invalid json format",
			policy: BgRoutingPolicy{
				Scope:             "org",
				ScopeID:           1,
				CapabilityPattern: "bg.llm.*",
				Strategy:          "fixed",
				RulesJSON:         `{`,
				Status:            "active",
			},
			wantErr: "invalid rules_json for fixed strategy",
		},
		{
			name: "invalid wildcard middle",
			policy: BgRoutingPolicy{
				Scope:             "org",
				ScopeID:           1,
				CapabilityPattern: "bg.*.chat",
				Strategy:          "fixed",
				RulesJSON:         `{"adapter_name": "ch1"}`,
				Status:            "active",
			},
			wantErr: "wildcard '*' must be the last segment",
		},
		{
			name: "invalid status",
			policy: BgRoutingPolicy{
				Scope:             "org",
				ScopeID:           1,
				CapabilityPattern: "bg.llm.*",
				Strategy:          "fixed",
				RulesJSON:         `{"adapter_name": "ch1"}`,
				Status:            "invalid_status",
			},
			wantErr: "status must be 'active' or 'disabled'",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.policy.Validate()
			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
