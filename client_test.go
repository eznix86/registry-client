package registryclient

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockLogger implements the Logger interface for testing
type mockLogger struct {
	debugCalls []logCall
	warnCalls  []logCall
	errorCalls []logCall
}

type logCall struct {
	msg  string
	args []any
}

func (m *mockLogger) Debug(msg string, args ...any) {
	m.debugCalls = append(m.debugCalls, logCall{msg: msg, args: args})
}

func (m *mockLogger) Info(msg string, args ...any) {}

func (m *mockLogger) Warn(msg string, args ...any) {
	m.warnCalls = append(m.warnCalls, logCall{msg: msg, args: args})
}

func (m *mockLogger) Error(msg string, args ...any) {
	m.errorCalls = append(m.errorCalls, logCall{msg: msg, args: args})
}

func TestBasicAuth_Apply(t *testing.T) {
	auth := BasicAuth{
		Username: "testuser",
		Password: "testpass",
	}

	req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	auth.Apply(req)

	username, password, ok := req.BasicAuth()
	require.True(t, ok, "BasicAuth not applied to request")
	assert.Equal(t, "testuser", username)
	assert.Equal(t, "testpass", password)
}

func TestBearerAuth_Apply(t *testing.T) {
	auth := BearerAuth{
		Token: "test-token-123",
	}

	req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	auth.Apply(req)

	authHeader := req.Header.Get("Authorization")
	assert.Equal(t, "Bearer test-token-123", authHeader)
}

func TestClient_Do_AppliesAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		require.True(t, ok, "BasicAuth not found in request")
		assert.Equal(t, "user", username)
		assert.Equal(t, "pass", password)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &Client{
		BaseURL: server.URL,
		Auth: BasicAuth{
			Username: "user",
			Password: "pass",
		},
	}

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
}

func TestClient_Do_NoAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _, ok := r.BasicAuth()
		assert.False(t, ok, "Unexpected BasicAuth in request")
		assert.Empty(t, r.Header.Get("Authorization"), "Unexpected Authorization header")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &Client{
		BaseURL: server.URL,
	}

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
}

func TestClient_DoWithRetry_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &Client{
		BaseURL: server.URL,
	}

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestClient_DoWithRetry_RetryableError(t *testing.T) {
	attemptCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		if attemptCount < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := &mockLogger{}
	client := &Client{
		BaseURL:      server.URL,
		MaxAttempts:  3,
		RetryBackoff: 10 * time.Millisecond,
		Logger:       logger,
	}

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	assert.Equal(t, 3, attemptCount, "Expected 3 attempts")
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify retry logs (2 retries = 2 warn calls)
	assert.Len(t, logger.warnCalls, 2, "Expected 2 warn logs for retries")
}

func TestClient_DoWithRetry_MaxRetriesExceeded(t *testing.T) {
	attemptCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	logger := &mockLogger{}
	client := &Client{
		BaseURL:      server.URL,
		MaxAttempts:  3,
		RetryBackoff: 10 * time.Millisecond,
		Logger:       logger,
	}

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err, "Should return response, not error")
	require.NotNil(t, resp, "Response should contain the last status code")
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode, "Should return actual status code from server")
	assert.Equal(t, 3, attemptCount, "Expected 3 attempts")

	assert.Len(t, logger.errorCalls, 1, "Expected 1 error log")
}

func TestClient_DoWithRetry_TooManyRequests(t *testing.T) {
	attemptCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		if attemptCount < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &Client{
		BaseURL:      server.URL,
		MaxAttempts:  2,
		RetryBackoff: 10 * time.Millisecond,
	}

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	assert.Equal(t, 2, attemptCount, "Expected 2 attempts")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestClient_MaxAttempts_Default(t *testing.T) {
	client := &Client{}
	assert.Equal(t, 1, client.maxAttempts())
}

func TestClient_MaxAttempts_Custom(t *testing.T) {
	client := &Client{MaxAttempts: 5}
	assert.Equal(t, 5, client.maxAttempts())
}

func TestClient_Backoff_Default(t *testing.T) {
	client := &Client{}
	assert.Equal(t, 100*time.Millisecond, client.backoff())
}

func TestClient_Backoff_Custom(t *testing.T) {
	customBackoff := 500 * time.Millisecond
	client := &Client{RetryBackoff: customBackoff}
	assert.Equal(t, customBackoff, client.backoff())
}

func TestClient_IsRetryableStatus(t *testing.T) {
	tests := []struct {
		statusCode int
		retryable  bool
	}{
		{http.StatusOK, false},
		{http.StatusCreated, false},
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, false},
		{http.StatusForbidden, false},
		{http.StatusNotFound, false},
		{http.StatusTooManyRequests, true},
		{http.StatusInternalServerError, true},
		{http.StatusBadGateway, true},
		{http.StatusServiceUnavailable, true},
		{http.StatusGatewayTimeout, true},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("status_%d", tt.statusCode), func(t *testing.T) {
			assert.Equal(t, tt.retryable, isRetryableStatus(tt.statusCode))
		})
	}
}

func TestClient_CalculateBackoff(t *testing.T) {
	baseBackoff := 100 * time.Millisecond

	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{1, 100 * time.Millisecond},  // 2^0 = 1
		{2, 200 * time.Millisecond},  // 2^1 = 2
		{3, 400 * time.Millisecond},  // 2^2 = 4
		{4, 800 * time.Millisecond},  // 2^3 = 8
		{5, 1600 * time.Millisecond}, // 2^4 = 16
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("attempt_%d", tt.attempt), func(t *testing.T) {
			assert.Equal(t, tt.expected, calculateBackoff(tt.attempt, baseBackoff))
		})
	}
}

func TestClient_CloseBody(t *testing.T) {
	logger := &mockLogger{}
	client := &Client{Logger: logger}

	body := io.NopCloser(strings.NewReader("test"))
	client.closeBody(body)

	assert.Empty(t, logger.debugCalls)
}

func TestClient_CloseBody_Error(t *testing.T) {
	logger := &mockLogger{}
	client := &Client{Logger: logger}

	// Test close with error
	errBody := &errorCloser{err: fmt.Errorf("close error")}
	client.closeBody(errBody)

	// Should log the error
	require.Len(t, logger.debugCalls, 1)
	assert.Equal(t, "Failed to close response body", logger.debugCalls[0].msg)
}

func TestClient_LogDebug(t *testing.T) {
	logger := &mockLogger{}
	client := &Client{Logger: logger}

	client.logDebug("test message", "key1", "value1", "key2", 123)

	require.Len(t, logger.debugCalls, 1)
	call := logger.debugCalls[0]
	assert.Equal(t, "test message", call.msg)
	assert.Len(t, call.args, 4)
}

func TestClient_LogDebug_NoLogger(t *testing.T) {
	client := &Client{}

	// Should not panic when logger is nil
	assert.NotPanics(t, func() {
		client.logDebug("test message", "key", "value")
	})
}

func TestClient_LogWarn(t *testing.T) {
	logger := &mockLogger{}
	client := &Client{Logger: logger}

	client.logWarn("warning message", "key", "value")

	require.Len(t, logger.warnCalls, 1)
	assert.Equal(t, "warning message", logger.warnCalls[0].msg)
}

func TestClient_LogError(t *testing.T) {
	logger := &mockLogger{}
	client := &Client{Logger: logger}

	client.logError("error message", "key", "value")

	require.Len(t, logger.errorCalls, 1)
	assert.Equal(t, "error message", logger.errorCalls[0].msg)
}

func TestClient_DoWithRetry_ExponentialBackoff(t *testing.T) {
	attemptCount := 0
	attemptTimes := []time.Time{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		attemptTimes = append(attemptTimes, time.Now())
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := &Client{
		BaseURL:      server.URL,
		MaxAttempts:  3,
		RetryBackoff: 50 * time.Millisecond,
	}

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err, "Should return response with status code")
	require.NotNil(t, resp, "Response should contain the last status code")
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode, "Should return actual status code from server")
	require.Len(t, attemptTimes, 3, "Expected 3 attempts")

	// Verify exponential backoff timing
	// First retry: ~50ms, Second retry: ~100ms
	firstDelay := attemptTimes[1].Sub(attemptTimes[0])
	secondDelay := attemptTimes[2].Sub(attemptTimes[1])

	// Allow some tolerance for timing
	assert.GreaterOrEqual(t, firstDelay, 40*time.Millisecond, "First retry too fast")
	assert.LessOrEqual(t, firstDelay, 70*time.Millisecond, "First retry too slow")

	assert.GreaterOrEqual(t, secondDelay, 80*time.Millisecond, "Second retry too fast")
	assert.LessOrEqual(t, secondDelay, 120*time.Millisecond, "Second retry too slow")
}

// errorCloser is a mock io.Closer that returns an error
type errorCloser struct {
	err error
}

func (e *errorCloser) Close() error {
	return e.err
}

func TestParseRetryAfter_Seconds(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected time.Duration
	}{
		{name: "valid seconds", header: "120", expected: 120 * time.Second},
		{name: "zero seconds", header: "0", expected: 0},
		{name: "negative seconds", header: "-10", expected: 0},
		{name: "empty header", header: "", expected: 0},
		{name: "invalid format", header: "invalid", expected: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				Header: http.Header{},
			}
			if tt.header != "" {
				resp.Header.Set("Retry-After", tt.header)
			}

			duration := parseRetryAfter(resp)
			assert.Equal(t, tt.expected, duration)
		})
	}
}

func TestParseRetryAfter_HTTPDate(t *testing.T) {
	// Test with a future date using UTC to avoid timezone issues
	futureTime := time.Now().UTC().Add(30 * time.Second)
	httpDate := futureTime.Format(http.TimeFormat)

	resp := &http.Response{
		Header: http.Header{},
	}
	resp.Header.Set("Retry-After", httpDate)

	duration := parseRetryAfter(resp)

	// Allow some tolerance for test execution time (25-35 seconds)
	assert.GreaterOrEqual(t, duration, 25*time.Second, "Duration too short")
	assert.LessOrEqual(t, duration, 35*time.Second, "Duration too long")
}

func TestParseRetryAfter_PastDate(t *testing.T) {
	// Test with a past date (should return 0) using UTC to avoid timezone issues
	pastTime := time.Now().UTC().Add(-30 * time.Second)
	httpDate := pastTime.Format(http.TimeFormat)

	resp := &http.Response{
		Header: http.Header{},
	}
	resp.Header.Set("Retry-After", httpDate)

	duration := parseRetryAfter(resp)
	assert.Equal(t, time.Duration(0), duration)
}

func TestClient_DoWithRetry_RetryAfterSeconds(t *testing.T) {
	attemptCount := 0
	attemptTimes := []time.Time{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		attemptTimes = append(attemptTimes, time.Now())

		if attemptCount < 3 {
			w.Header().Set("Retry-After", "1") // 1 second
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := &mockLogger{}
	client := &Client{
		BaseURL:      server.URL,
		MaxAttempts:  3,
		RetryBackoff: 100 * time.Millisecond, // Should be overridden by Retry-After
		Logger:       logger,
	}

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	assert.Equal(t, 3, attemptCount, "Expected 3 attempts")
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify Retry-After was respected (should be ~1 second, not 100ms)
	require.Len(t, attemptTimes, 3)
	firstDelay := attemptTimes[1].Sub(attemptTimes[0])

	// Allow some tolerance for timing
	assert.GreaterOrEqual(t, firstDelay, 900*time.Millisecond, "Retry-After not respected")
	assert.LessOrEqual(t, firstDelay, 1200*time.Millisecond, "Delay too long")

	// Verify logging mentions Retry-After
	require.Len(t, logger.warnCalls, 2)
	// Check that at least one log contains retry_after
	hasRetryAfter := false
	for _, call := range logger.warnCalls {
		for i := 0; i < len(call.args); i += 2 {
			if i+1 < len(call.args) && call.args[i] == "source" {
				if call.args[i+1] == "Retry-After header" {
					hasRetryAfter = true
					break
				}
			}
		}
	}
	assert.True(t, hasRetryAfter, "Expected Retry-After to be logged")
}

func TestClient_DoWithRetry_NoRetryAfter(t *testing.T) {
	attemptCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		if attemptCount < 2 {
			// No Retry-After header - should use exponential backoff
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := &mockLogger{}
	client := &Client{
		BaseURL:      server.URL,
		MaxAttempts:  2,
		RetryBackoff: 50 * time.Millisecond,
		Logger:       logger,
	}

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	assert.Equal(t, 2, attemptCount, "Expected 2 attempts")
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify standard backoff was used (not Retry-After)
	require.Len(t, logger.warnCalls, 1)
	call := logger.warnCalls[0]

	// Check that it logged "backoff" not "retry_after"
	hasBackoff := false
	for i := 0; i < len(call.args); i += 2 {
		if i+1 < len(call.args) && call.args[i] == "backoff" {
			hasBackoff = true
			break
		}
	}
	assert.True(t, hasBackoff, "Expected backoff to be logged")
}
