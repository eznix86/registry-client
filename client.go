package registryclient

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// Auth defines the interface for applying authentication to HTTP requests
type Auth interface {
	Apply(req *http.Request)
}

type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// BasicAuth implements HTTP Basic Authentication
type BasicAuth struct {
	Username string
	Password string
}

func (b BasicAuth) Apply(req *http.Request) {
	req.SetBasicAuth(b.Username, b.Password)
}

// BearerAuth implements HTTP Bearer Token Authentication
type BearerAuth struct {
	Token string
}

func (b BearerAuth) Apply(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+b.Token)
}

// Client wraps http.Client with registry-specific configuration
type Client struct {
	http.Client
	BaseURL      string
	Auth         Auth
	RetryBackoff time.Duration // Initial backoff duration for retries
	MaxAttempts  int           // Maximum number of retry attempts (0 = no retries)
	Logger       Logger        // Optional logger (nil = no logging)
}

// Do applies auth before performing the request with retry logic
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	if c.Auth != nil {
		c.Auth.Apply(req)
	}
	return c.doWithRetry(req)
}

// retryState holds the state for a retry attempt
type retryState struct {
	lastResp *http.Response
	lastErr  error
}

// doWithRetry executes the request with exponential backoff retry logic
func (c *Client) doWithRetry(req *http.Request) (*http.Response, error) {
	maxAttempts := c.maxAttempts()
	backoff := c.backoff()
	state := &retryState{}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		resp, err := c.Client.Do(req)

		if shouldReturnImmediately(resp, err) {
			return resp, nil
		}

		c.updateRetryState(state, resp, err)

		if shouldRetry(attempt, maxAttempts) {
			sleepDuration := getRetryDelay(state.lastResp, attempt, backoff)
			c.logRetryAttempt(req, attempt, maxAttempts, state.lastErr, sleepDuration, state.lastResp)
			time.Sleep(sleepDuration)
		}
	}

	return c.handleMaxRetriesExceeded(req, maxAttempts, state)
}

// shouldReturnImmediately checks if we should return the response without retrying
func shouldReturnImmediately(resp *http.Response, err error) bool {
	if err != nil {
		return false
	}
	return !isRetryableStatus(resp.StatusCode)
}

// updateRetryState updates the retry state with the latest response/error
func (c *Client) updateRetryState(state *retryState, resp *http.Response, err error) {
	if err == nil {
		// Close previous response body if exists
		if state.lastResp != nil {
			c.closeBody(state.lastResp.Body)
		}
		state.lastResp = resp
		state.lastErr = fmt.Errorf("retryable status code: %d", resp.StatusCode)
	} else {
		state.lastErr = err
	}
}

// shouldRetry determines if another retry attempt should be made
func shouldRetry(attempt, maxAttempts int) bool {
	return attempt < maxAttempts
}

// getRetryDelay calculates the delay before the next retry attempt
func getRetryDelay(resp *http.Response, attempt int, backoff time.Duration) time.Duration {
	if resp != nil {
		if retryAfter := parseRetryAfter(resp); retryAfter > 0 {
			return retryAfter
		}
	}
	return calculateBackoff(attempt, backoff)
}

// logRetryAttempt logs the retry attempt with appropriate context
func (c *Client) logRetryAttempt(req *http.Request, attempt, maxAttempts int, err error, sleepDuration time.Duration, resp *http.Response) {
	if resp != nil && parseRetryAfter(resp) > 0 {
		c.logRetryWithRetryAfter(req, attempt, maxAttempts, err, sleepDuration)
	} else {
		c.logRetry(req, attempt, maxAttempts, err, c.backoff())
	}
}

// handleMaxRetriesExceeded handles the case when all retry attempts are exhausted
func (c *Client) handleMaxRetriesExceeded(req *http.Request, maxAttempts int, state *retryState) (*http.Response, error) {
	c.logMaxRetriesExceeded(req, maxAttempts, state.lastErr)

	// If we have a response with a retryable status, return it instead of error
	if state.lastResp != nil {
		return state.lastResp, nil
	}

	return nil, fmt.Errorf("max retries exceeded: %w", state.lastErr)
}

// maxAttempts returns the maximum number of attempts (at least 1)
func (c *Client) maxAttempts() int {
	if c.MaxAttempts <= 0 {
		return 1
	}
	return c.MaxAttempts
}

// backoff returns the initial backoff duration with default fallback
func (c *Client) backoff() time.Duration {
	if c.RetryBackoff <= 0 {
		return 100 * time.Millisecond
	}
	return c.RetryBackoff
}

// isRetryableStatus returns true if the status code warrants a retry
func isRetryableStatus(statusCode int) bool {
	return statusCode >= 500 || statusCode == http.StatusTooManyRequests
}

// calculateBackoff returns the backoff duration for the given attempt using exponential backoff
func calculateBackoff(attempt int, baseBackoff time.Duration) time.Duration {
	exp := max(attempt-1, 0)
	return baseBackoff * time.Duration(1<<exp)
}

// parseRetryAfter parses the Retry-After header and returns the duration to wait.
// Returns 0 if the header is not present or cannot be parsed.
// Supports both delay-seconds (e.g., "120") and HTTP-date (e.g., "Wed, 21 Oct 2015 07:28:00 GMT")
func parseRetryAfter(resp *http.Response) time.Duration {
	retryAfter := resp.Header.Get("Retry-After")
	if retryAfter == "" {
		return 0
	}

	// Try parsing as seconds
	if seconds, err := strconv.ParseInt(retryAfter, 10, 64); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}

	// Try parsing as HTTP-date
	if t, err := http.ParseTime(retryAfter); err == nil {
		if duration := time.Until(t); duration > 0 {
			return duration
		}
	}

	return 0
}

// logRetry logs a retry attempt if a logger is configured
func (c *Client) logRetry(req *http.Request, attempt, maxAttempts int, err error, backoff time.Duration) {
	sleepDuration := calculateBackoff(attempt, backoff)
	c.logWarn("Retrying registry request",
		"method", req.Method,
		"url", req.URL.String(),
		"attempt", attempt+1,
		"max_attempts", maxAttempts,
		"reason", err.Error(),
		"backoff", sleepDuration.String(),
	)
}

// logRetryWithRetryAfter logs a retry attempt with Retry-After header if a logger is configured
func (c *Client) logRetryWithRetryAfter(req *http.Request, attempt, maxAttempts int, err error, retryAfter time.Duration) {
	c.logWarn("Retrying registry request",
		"method", req.Method,
		"url", req.URL.String(),
		"attempt", attempt+1,
		"max_attempts", maxAttempts,
		"reason", err.Error(),
		"retry_after", retryAfter.String(),
		"source", "Retry-After header",
	)
}

// logMaxRetriesExceeded logs when max retries are exceeded if a logger is configured
func (c *Client) logMaxRetriesExceeded(req *http.Request, maxAttempts int, err error) {
	c.logError("Registry request max retries exceeded",
		"method", req.Method,
		"url", req.URL.String(),
		"attempts", maxAttempts,
		"last_error", err.Error(),
	)
}

// closeBody closes the response body and logs any error if a logger is configured
func (c *Client) closeBody(body io.Closer) {
	if err := body.Close(); err != nil {
		c.logDebug("Failed to close response body", "error", err.Error())
	}
}

// logDebug logs a debug message if a logger is configured
func (c *Client) logDebug(msg string, args ...any) {
	if c.Logger != nil {
		c.Logger.Debug(msg, args...)
	}
}

// logWarn logs a warning message if a logger is configured
func (c *Client) logWarn(msg string, args ...any) {
	if c.Logger != nil {
		c.Logger.Warn(msg, args...)
	}
}

// logError logs an error message if a logger is configured
func (c *Client) logError(msg string, args ...any) {
	if c.Logger != nil {
		c.Logger.Error(msg, args...)
	}
}
