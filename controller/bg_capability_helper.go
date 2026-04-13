package controller

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

// CapabilityWithPricing extends BgCapability with live pricing info from ratio_setting.
// Shared between admin and dev capability list endpoints.
type CapabilityWithPricing struct {
	model.BgCapability
	PricingMode string  `json:"pricing_mode"` // "ratio" | "price" | "none"
	UnitPrice   float64 `json:"unit_price"`
}

// enrichCapabilitiesWithPricing applies live pricing lookup to a list of capabilities.
// Used by both AdminListBgCapabilities and DevListBgCapabilities.
func enrichCapabilitiesWithPricing(caps []*model.BgCapability) []CapabilityWithPricing {
	enriched := make([]CapabilityWithPricing, len(caps))
	for i, cap := range caps {
		enriched[i].BgCapability = *cap
		pricing := service.LookupPricing(cap.CapabilityName, "hosted")
		if pricing != nil && pricing.UnitPrice > 0 {
			if pricing.PricingMode == "per_call" {
				enriched[i].PricingMode = "price"
			} else {
				enriched[i].PricingMode = "ratio"
			}
			enriched[i].UnitPrice = pricing.UnitPrice
		} else {
			enriched[i].PricingMode = "none"
		}
	}
	return enriched
}
