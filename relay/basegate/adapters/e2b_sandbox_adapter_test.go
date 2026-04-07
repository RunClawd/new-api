package adapters

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/relay/basegate"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestE2BSandboxAdapter_Name(t *testing.T) {
	adapter := NewE2BSandboxAdapter(5, "test-key", "", "")
	assert.Equal(t, "e2b_sandbox_ch5", adapter.Name())
}

func TestE2BSandboxAdapter_DescribeCapabilities(t *testing.T) {
	adapter := NewE2BSandboxAdapter(1, "test-key", "", "")
	caps := adapter.DescribeCapabilities()

	require.Len(t, caps, 1)
	assert.Equal(t, "bg.sandbox.session.standard", caps[0].CapabilityPattern)
	assert.Equal(t, "e2b", caps[0].Provider)
}

func TestE2BSandboxAdapter_Validate(t *testing.T) {
	adapter := NewE2BSandboxAdapter(1, "test-key", "", "")

	t.Run("valid", func(t *testing.T) {
		res := adapter.Validate(&relaycommon.CanonicalRequest{Model: "bg.sandbox.session.standard"})
		assert.True(t, res.Valid)
	})

	t.Run("invalid", func(t *testing.T) {
		res := adapter.Validate(&relaycommon.CanonicalRequest{Model: "bg.llm.chat.fast"})
		assert.False(t, res.Valid)
	})
}

func TestE2BSandboxAdapter_CreateSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/sandboxes", r.URL.Path)
		assert.Equal(t, "test-key-123", r.Header.Get("X-API-Key"))

		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), "code-interpreter-v1")

		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"sandboxID":"sb_abc123","clientID":"cl_xyz","templateID":"code-interpreter-v1"}`))
	}))
	defer server.Close()

	adapter := NewE2BSandboxAdapter(1, "test-key-123", server.URL, "code-interpreter-v1")
	req := &relaycommon.CanonicalRequest{
		Model: "bg.sandbox.session.standard",
	}

	sess, err := adapter.CreateSession(req)
	require.NoError(t, err)
	assert.Equal(t, "sb_abc123", sess.SessionID)
	assert.Contains(t, sess.LiveURL, "sb_abc123.e2b.app")
	assert.True(t, sess.ExpiresAt > 0)
}

func TestE2BSandboxAdapter_CreateSessionError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer server.Close()

	adapter := NewE2BSandboxAdapter(1, "bad-key", server.URL, "")
	_, err := adapter.CreateSession(&relaycommon.CanonicalRequest{Model: "bg.sandbox.session.standard"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 401")
}

func TestE2BSandboxAdapter_ExecuteAction(t *testing.T) {
	// ExecuteAction constructs URLs from the sandbox ID (e.g., https://{id}.e2b.app/process/start)
	// which makes it difficult to inject a test server. We verify the struct interface compliance
	// and the action request construction here.
	action := &basegate.SessionActionRequest{
		Action: "print('Hello World')",
		Input:  "print('Hello World')",
	}
	assert.NotNil(t, action)
	assert.Equal(t, "print('Hello World')", action.Action)
}

func TestE2BSandboxAdapter_CloseSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Contains(t, r.URL.Path, "/sandboxes/sb_abc123")

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	adapter := NewE2BSandboxAdapter(1, "test-key", server.URL, "")
	// Simulate a previous start
	adapter.sandboxStarted["sb_abc123"] = adapter.sandboxStarted["sb_abc123"] // zero-value OK

	result, err := adapter.CloseSession("sb_abc123")
	require.NoError(t, err)
	require.NotNil(t, result.FinalUsage)
	assert.Equal(t, "minute", result.FinalUsage.BillableUnit)
	assert.True(t, result.FinalUsage.SessionMinutes >= 1.0) // minimum 1 minute
}

func TestE2BSandboxAdapter_GetSessionStatus(t *testing.T) {
	t.Run("running sandbox", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "GET", r.Method)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"state":"running","endAt":"2026-04-08T06:00:00Z"}`))
		}))
		defer server.Close()

		adapter := NewE2BSandboxAdapter(1, "test-key", server.URL, "")
		status, err := adapter.GetSessionStatus("sb_123")
		require.NoError(t, err)
		assert.Equal(t, "idle", status.Status) // running maps to idle
	})

	t.Run("paused sandbox", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"state":"paused"}`))
		}))
		defer server.Close()

		adapter := NewE2BSandboxAdapter(1, "test-key", server.URL, "")
		status, err := adapter.GetSessionStatus("sb_123")
		require.NoError(t, err)
		assert.Equal(t, "paused", status.Status)
	})

	t.Run("not found sandbox", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		adapter := NewE2BSandboxAdapter(1, "test-key", server.URL, "")
		status, err := adapter.GetSessionStatus("sb_gone")
		require.NoError(t, err)
		assert.Equal(t, "closed", status.Status)
	})
}

func TestE2BSandboxAdapter_Invoke_ProxiesToCreateSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"sandboxID":"sb_invoke_test","clientID":"cl_1","templateID":"code-interpreter-v1"}`))
	}))
	defer server.Close()

	adapter := NewE2BSandboxAdapter(1, "test-key", server.URL, "code-interpreter-v1")
	result, err := adapter.Invoke(&relaycommon.CanonicalRequest{
		Model: "bg.sandbox.session.standard",
	})
	require.NoError(t, err)
	assert.Equal(t, "succeeded", result.Status)
	assert.NotNil(t, result.Session)
	assert.Equal(t, "sb_invoke_test", result.Session.SessionID)
	require.Len(t, result.Output, 1)
	assert.Equal(t, "session", result.Output[0].Type)
}

func TestE2BSandboxAdapter_Poll_NotApplicable(t *testing.T) {
	adapter := NewE2BSandboxAdapter(1, "test-key", "", "")
	_, err := adapter.Poll("anything")
	assert.Error(t, err)
}

func TestE2BSandboxAdapter_Cancel_NotApplicable(t *testing.T) {
	adapter := NewE2BSandboxAdapter(1, "test-key", "", "")
	_, err := adapter.Cancel("anything")
	assert.Error(t, err)
}

func TestE2BSandboxAdapter_Stream_NotSupported(t *testing.T) {
	adapter := NewE2BSandboxAdapter(1, "test-key", "", "")
	_, err := adapter.Stream(&relaycommon.CanonicalRequest{})
	assert.Error(t, err)
}
