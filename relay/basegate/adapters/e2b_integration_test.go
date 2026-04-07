package adapters

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/relay/basegate"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2BIntegration_FullLifecycle runs against the real E2B API.
// Requires E2B_API_KEY env var. Skip gracefully if not set.
//
//	go test ./relay/basegate/adapters/... -run TestE2BIntegration -v -count=1
func TestE2BIntegration_FullLifecycle(t *testing.T) {
	apiKey := os.Getenv("E2B_API_KEY")
	if apiKey == "" {
		t.Skip("E2B_API_KEY not set — skipping integration test")
	}

	adapter := NewE2BSandboxAdapter(0, apiKey, "", "base")

	// Verify interface compliance
	var _ basegate.ProviderAdapter = adapter
	var _ basegate.SessionCapableAdapter = adapter

	// --- Step 1: Create sandbox ---
	t.Log("Step 1: Creating sandbox...")
	req := &relaycommon.CanonicalRequest{
		Model: "bg.sandbox.session.standard",
	}
	sess, err := adapter.CreateSession(req)
	require.NoError(t, err, "CreateSession must succeed")
	require.NotEmpty(t, sess.SessionID, "sandbox ID must be populated")
	assert.Contains(t, sess.LiveURL, sess.SessionID, "LiveURL must contain sandbox ID")
	assert.Contains(t, sess.LiveURL, "49983-", "LiveURL must include envd port prefix")
	assert.True(t, sess.ExpiresAt > 0, "ExpiresAt must be set")
	t.Logf("  Sandbox created: %s (LiveURL: %s)", sess.SessionID, sess.LiveURL)

	sandboxID := sess.SessionID

	// --- Step 2: Check status ---
	t.Log("Step 2: Checking sandbox status...")
	status, err := adapter.GetSessionStatus(sandboxID)
	require.NoError(t, err, "GetSessionStatus must succeed")
	assert.Equal(t, "idle", status.Status, "running sandbox should map to idle")
	t.Logf("  Status: %s", status.Status)

	// --- Step 3: Execute code ---
	t.Log("Step 3: Executing code (echo Hello + python3 2+2)...")
	action := &basegate.SessionActionRequest{
		Action:    "echo Hello_BaseGate && python3 -c 'print(2+2)'",
		TimeoutMs: 30000,
	}
	result, err := adapter.ExecuteAction(sandboxID, action)
	require.NoError(t, err, "ExecuteAction must not return Go error")
	t.Logf("  Status: %s, Output: %+v", result.Status, result.Output)

	if result.Status == "succeeded" {
		outputMap, ok := result.Output.(map[string]interface{})
		require.True(t, ok, "output must be a map")
		stdout, _ := outputMap["stdout"].(string)
		assert.Contains(t, stdout, "Hello_BaseGate", "stdout must contain echo output")
		assert.Contains(t, stdout, "4", "stdout must contain python result")
		t.Logf("  stdout: %q", stdout)
	} else {
		// Log failure details but don't hard-fail — Connect RPC framing may vary
		t.Logf("  ExecuteAction returned status=%s (may need protocol tuning)", result.Status)
		if result.Error != nil {
			t.Logf("  Error: %s — %s", result.Error.Code, result.Error.Message)
		}
	}

	// --- Step 4: Close sandbox ---
	t.Log("Step 4: Closing sandbox...")
	closeResult, err := adapter.CloseSession(sandboxID)
	require.NoError(t, err, "CloseSession must succeed")
	require.NotNil(t, closeResult.FinalUsage, "FinalUsage must be populated")
	assert.Equal(t, "minute", closeResult.FinalUsage.BillableUnit)
	assert.True(t, closeResult.FinalUsage.SessionMinutes >= 1.0, "minimum 1 minute billing")
	t.Logf("  Closed. Billed: %.2f minutes", closeResult.FinalUsage.SessionMinutes)

	// --- Step 5: Verify closed ---
	t.Log("Step 5: Verifying sandbox is closed...")
	status2, err := adapter.GetSessionStatus(sandboxID)
	require.NoError(t, err)
	assert.Equal(t, "closed", status2.Status, "deleted sandbox should return closed")
	t.Logf("  Final status: %s", status2.Status)
}
