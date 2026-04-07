package adapters

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKlingVideoAdapter_DescribeCapabilities(t *testing.T) {
	adapter := NewKlingVideoAdapter(1, "test-kling", "ak|sk", "")
	caps := adapter.DescribeCapabilities()

	assert.Len(t, caps, 2)
	names := map[string]bool{}
	for _, c := range caps {
		names[c.CapabilityPattern] = true
		assert.Equal(t, "kling", c.Provider)
		assert.True(t, c.SupportsAsync)
		assert.False(t, c.SupportsStreaming)
	}
	assert.True(t, names["bg.video.generate.standard"])
	assert.True(t, names["bg.video.generate.pro"])
}

func TestKlingVideoAdapter_Validate(t *testing.T) {
	adapter := NewKlingVideoAdapter(1, "test-kling", "ak|sk", "")

	t.Run("valid model", func(t *testing.T) {
		res := adapter.Validate(&relaycommon.CanonicalRequest{Model: "bg.video.generate.standard"})
		assert.True(t, res.Valid)
	})

	t.Run("invalid model", func(t *testing.T) {
		res := adapter.Validate(&relaycommon.CanonicalRequest{Model: "bg.llm.chat.fast"})
		assert.False(t, res.Valid)
		assert.NotNil(t, res.Error)
	})
}

func TestKlingVideoAdapter_Name(t *testing.T) {
	adapter := NewKlingVideoAdapter(42, "test-kling", "ak|sk", "")
	assert.Equal(t, "kling_native_ch42", adapter.Name())
}

func TestKlingVideoAdapter_InvokeSuccess(t *testing.T) {
	// Mock Kling API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.True(t, strings.Contains(r.URL.Path, "/v1/videos/text2video"))

		// Verify auth header
		auth := r.Header.Get("Authorization")
		assert.True(t, strings.HasPrefix(auth, "Bearer "))

		// Read body to verify payload
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), "kling-v1")

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"code":0,"data":{"task_id":"task_12345"}}`))
	}))
	defer server.Close()

	adapter := NewKlingVideoAdapter(1, "test-kling", "ak|sk", server.URL)
	req := &relaycommon.CanonicalRequest{
		Model: "bg.video.generate.standard",
		Input: map[string]interface{}{
			"prompt": "A cat dancing in the rain",
		},
	}

	result, err := adapter.Invoke(req)
	require.NoError(t, err)
	assert.Equal(t, "accepted", result.Status)
	assert.Equal(t, "task_12345", result.ProviderRequestID)
	assert.Equal(t, 15000, result.PollAfterMs)
}

func TestKlingVideoAdapter_InvokeProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"code":1001,"message":"invalid parameter"}`))
	}))
	defer server.Close()

	adapter := NewKlingVideoAdapter(1, "test-kling", "ak|sk", server.URL)
	result, err := adapter.Invoke(&relaycommon.CanonicalRequest{
		Model: "bg.video.generate.standard",
		Input: map[string]interface{}{},
	})
	require.NoError(t, err) // adapter error, not Go error
	assert.Equal(t, "failed", result.Status)
	assert.Contains(t, result.Error.Message, "invalid parameter")
}

func TestKlingVideoAdapter_PollStates(t *testing.T) {
	tests := []struct {
		name           string
		responseJSON   string
		expectedStatus string
		expectedPoll   int
	}{
		{
			name:           "submitted",
			responseJSON:   `{"code":0,"data":{"task_status":"submitted"}}`,
			expectedStatus: "accepted",
			expectedPoll:   30000,
		},
		{
			name:           "processing",
			responseJSON:   `{"code":0,"data":{"task_status":"processing"}}`,
			expectedStatus: "running",
			expectedPoll:   10000,
		},
		{
			name:           "succeed with video",
			responseJSON:   `{"code":0,"data":{"task_status":"succeed","final_unit_deduction":"10","task_result":{"videos":[{"url":"https://example.com/video.mp4","duration":"5.0"}]}}}`,
			expectedStatus: "succeeded",
			expectedPoll:   0,
		},
		{
			name:           "failed",
			responseJSON:   `{"code":0,"data":{"task_status":"failed","task_status_msg":"content violation"}}`,
			expectedStatus: "failed",
			expectedPoll:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "GET", r.Method)
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(tt.responseJSON))
			}))
			defer server.Close()

			adapter := NewKlingVideoAdapter(1, "test-kling", "ak|sk", server.URL)
			result, err := adapter.Poll("task_999")
			require.NoError(t, err)
			assert.Equal(t, tt.expectedStatus, result.Status)

			if tt.expectedPoll > 0 {
				assert.Equal(t, tt.expectedPoll, result.PollAfterMs)
			}
		})
	}
}

func TestKlingVideoAdapter_PollSuccess_VideoOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"code":0,"data":{"task_status":"succeed","task_result":{"videos":[{"url":"https://cdn.kling.com/v1.mp4","duration":"7.5"}]},"final_unit_deduction":"8"}}`))
	}))
	defer server.Close()

	adapter := NewKlingVideoAdapter(1, "test-kling", "ak|sk", server.URL)
	result, err := adapter.Poll("task_ok")
	require.NoError(t, err)
	assert.Equal(t, "succeeded", result.Status)
	require.Len(t, result.Output, 1)
	assert.Equal(t, "video", result.Output[0].Type)
	assert.Equal(t, "https://cdn.kling.com/v1.mp4", result.Output[0].Content)
	require.NotNil(t, result.RawUsage)
	assert.Equal(t, 7.5, result.RawUsage.DurationSec)
	assert.Equal(t, "second", result.RawUsage.BillableUnit)
}

func TestKlingVideoAdapter_Stream_NotSupported(t *testing.T) {
	adapter := NewKlingVideoAdapter(1, "test-kling", "ak|sk", "")
	_, err := adapter.Stream(&relaycommon.CanonicalRequest{})
	assert.Error(t, err)
}

func TestKlingVideoAdapter_Cancel_NotSupported(t *testing.T) {
	adapter := NewKlingVideoAdapter(1, "test-kling", "ak|sk", "")
	_, err := adapter.Cancel("task_123")
	assert.Error(t, err)
}

func TestCreateKlingJWT(t *testing.T) {
	t.Run("relay key passthrough", func(t *testing.T) {
		token, err := CreateKlingJWT("sk-test-key-123")
		require.NoError(t, err)
		assert.Equal(t, "sk-test-key-123", token)
	})

	t.Run("jwt generation", func(t *testing.T) {
		token, err := CreateKlingJWT("my_access|my_secret")
		require.NoError(t, err)
		assert.NotEmpty(t, token)
		// JWT tokens have 3 parts separated by dots
		parts := strings.SplitN(token, ".", 3)
		assert.Len(t, parts, 3)
	})

	t.Run("invalid format", func(t *testing.T) {
		_, err := CreateKlingJWT("just-a-key")
		assert.Error(t, err)
	})
}
