package service

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/basegate"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

func serializeFeeConfigJSON(feeConfig *relaycommon.BYOFeeConfig) string {
	if feeConfig == nil {
		return ""
	}
	b, err := common.Marshal(feeConfig)
	if err != nil {
		return ""
	}
	return string(b)
}

func adapterResultAccepted(result *relaycommon.AdapterResult) bool {
	if result == nil {
		return false
	}
	switch result.Status {
	case "accepted", "queued", "running", "succeeded", "completed", "success":
		return true
	default:
		return false
	}
}

// bestEffortAdoptWinningAdapterMetadata updates response display fields to reflect the
// adapter that actually won dispatch. Terminal billing still reads from attempts.
func bestEffortAdoptWinningAdapterMetadata(responseDBID int64, adapter basegate.ResolvedAdapter) {
	billingSource := adapter.BillingSource
	if billingSource == "" {
		billingSource = "hosted"
	}
	updates := map[string]interface{}{
		"billing_source":    billingSource,
		"billing_mode":      billingSource,
		"byo_credential_id": adapter.BYOCredentialID,
		"fee_config_json":   serializeFeeConfigJSON(adapter.FeeConfig),
	}
	if err := model.DB.Model(&model.BgResponse{}).Where("id = ?", responseDBID).Updates(updates).Error; err != nil {
		common.SysError("bg_response: failed to adopt winning adapter metadata: " + err.Error())
	}
	if adapter.BYOCredentialID > 0 {
		if err := model.TouchBgBYOCredentialLastUsed(adapter.BYOCredentialID, time.Now().Unix()); err != nil {
			common.SysError("bg_response: failed to update BYO credential last_used_at: " + err.Error())
		}
	}
}
