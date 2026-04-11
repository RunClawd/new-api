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
			name: "valid byo_first",
			policy: BgRoutingPolicy{
				Scope:             "org",
				ScopeID:           1,
				CapabilityPattern: "bg.llm.*",
				Strategy:          "byo_first",
				RulesJSON:         `{"byo_adapter_pattern": "*byo*"}`,
				Status:            "active",
			},
		},
		{
			name: "invalid byo_first missing pattern",
			policy: BgRoutingPolicy{
				Scope:             "org",
				ScopeID:           1,
				CapabilityPattern: "bg.llm.*",
				Strategy:          "byo_first",
				RulesJSON:         `{}`,
				Status:            "active",
			},
			wantErr: "byo_first strategy requires 'byo_adapter_pattern'",
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
