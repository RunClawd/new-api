package service

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/basegate"
)

// BgSessionWorkerConfig configures the session timeout poller.
type BgSessionWorkerConfig struct {
	ScanInterval     time.Duration
	ExpiredBatchSize int
	IdleBatchSize    int
	GracePeriodSec   int64
}

// DefaultSessionWorkerConfig provides sane defaults.
var DefaultSessionWorkerConfig = BgSessionWorkerConfig{
	ScanInterval:     30 * time.Second,
	ExpiredBatchSize: 50,
	IdleBatchSize:    50,
	GracePeriodSec:   60,
}

// BgSessionWorker implements the background polling strictly to enforce timeouts.
type BgSessionWorker struct {
	config BgSessionWorkerConfig
	stopCh chan struct{}
}

func NewBgSessionWorker(cfg BgSessionWorkerConfig) *BgSessionWorker {
	return &BgSessionWorker{
		config: cfg,
		stopCh: make(chan struct{}),
	}
}

func (w *BgSessionWorker) Start() {
	go func() {
		ticker := time.NewTicker(w.config.ScanInterval)
		defer ticker.Stop()

		for {
			select {
			case <-w.stopChIfAny():
				return
			case <-ticker.C:
				w.ScanIdle()
				w.ScanExpired()
			}
		}
	}()
}

func (w *BgSessionWorker) Stop() {
	close(w.stopCh)
}

func (w *BgSessionWorker) stopChIfAny() <-chan struct{} {
	return w.stopCh
}

// ScanIdle processes Phase 1 timeouts: Active -> Idle
func (w *BgSessionWorker) ScanIdle() {
	now := time.Now().Unix()
	
	sessions, err := model.GetIdleSessions(now, w.config.IdleBatchSize)
	if err != nil {
		common.SysError(fmt.Sprintf("session_worker: failed to get idle sessions: %v", err))
		return
	}

	for _, session := range sessions {
		// Attempt CAS update from Active to Idle
		success, updateErr := session.CASUpdateStatus(model.BgSessionStatusActive, session.StatusVersion, model.BgSessionStatusIdle)
		if updateErr != nil {
			common.SysError(fmt.Sprintf("session_worker: failed to mark %s idle: %v", session.SessionID, updateErr))
			continue
		}
		if success {
			common.SysLog(fmt.Sprintf("session_worker: session %s transitioned to idle (last action at %d)", session.SessionID, session.LastActionAt))
		}
	}
}

// ScanExpired processes Phase 2 timeouts: Active/Idle -> Expired (Closed + Billing)
func (w *BgSessionWorker) ScanExpired() {
	now := time.Now().Unix()
	
	// Grace period protects against edge-case clock drifts
	cutoff := now - w.config.GracePeriodSec
	
	sessions, err := model.GetExpiredSessions(cutoff, w.config.ExpiredBatchSize)
	if err != nil {
		common.SysError(fmt.Sprintf("session_worker: failed to get expired sessions: %v", err))
		return
	}

	for _, session := range sessions {
		// First transition directly to expired in DB to prevent new actions
		success, _ := session.CASUpdateStatus(session.Status, session.StatusVersion, model.BgSessionStatusExpired)
		
		if !success {
			continue // Somebody else processed it
		}

		// Adapter best-effort termination
		providerAdapter := basegate.LookupAdapterByName(session.AdapterName)
		if sessionAdapter, ok := providerAdapter.(basegate.SessionCapableAdapter); ok {
			sessionAdapter.CloseSession(session.ProviderSessionID)
		}

		// Trigger Phase 3 Billing!
		if err := FinalizeSessionBilling(&session); err != nil {
			common.SysError(fmt.Sprintf("session_worker: FinalizeSessionBilling failed for expired %s: %v", session.SessionID, err))
		} else {
			common.SysLog(fmt.Sprintf("session_worker: session %s expired and closed (expires_at %d)", session.SessionID, session.ExpiresAt))
		}
	}
}
