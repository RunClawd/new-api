package model

import (
	"github.com/QuantumNous/new-api/common"
)

type BgAuditLog struct {
	ID         int64  `gorm:"primaryKey"`
	OrgID      int    `gorm:"index"`
	RequestID  string `gorm:"type:varchar(100);index"`
	ResponseID string `gorm:"type:varchar(100);index"`
	EventType  string `gorm:"type:varchar(50);index"`
	DetailJSON string `gorm:"type:text"` // 结构化 JSON
	CreatedAt  int64  `gorm:"autoCreateTime"`
}

func RecordBgAuditLog(orgID int, requestID string, responseID string, eventType string, detail map[string]interface{}) error {
	var detailStr string
	if detail != nil {
		b, err := common.Marshal(detail)
		if err == nil {
			detailStr = string(b)
		} else {
			detailStr = "{}"
		}
	} else {
		detailStr = "{}"
	}

	auditLog := &BgAuditLog{
		OrgID:      orgID,
		RequestID:  requestID,
		ResponseID: responseID,
		EventType:  eventType,
		DetailJSON: detailStr,
	}

	// Async insert to avoid blocking main flow
	go func() {
		defer func() {
			if r := recover(); r != nil {
				common.SysError("RecordBgAuditLog panic: " + common.Interface2String(r))
			}
		}()
		err := DB.Create(auditLog).Error
		if err != nil {
			common.SysError("failed to record bg audit log: " + err.Error())
		}
	}()

	return nil
}
