package registryclient

import (
	"fmt"
	"io"
	"net/http"
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

// doWithRetry executes the request with exponential backoff retry logic
func (c *Client) doWithRetry(req *http.Request) (*http.Response, error) {
	maxAttempts := c.maxAttempts()
	backoff := c.backoff()

	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		resp, err := c.Client.Do(req)

		if err == nil && !c.isRetryableStatus(resp.StatusCode) {
			return resp, nil
		}

		// Handle retryable error
		if err == nil {
			c.closeBody(resp.Body)
			lastErr = fmt.Errorf("retryable status code: %d", resp.StatusCode)
		} else {
			lastErr = err
		}

		if attempt < maxAttempts {
			c.logRetry(req, attempt, maxAttempts, lastErr, backoff)
			time.Sleep(c.calculateBackoff(attempt, backoff))
		}
	}

	c.logMaxRetriesExceeded(req, maxAttempts, lastErr)
	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
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
func (c *Client) isRetryableStatus(statusCode int) bool {
	return statusCode >= 500 || statusCode == http.StatusTooManyRequests
}

// calculateBackoff returns the backoff duration for the given attempt using exponential backoff
func (c *Client) calculateBackoff(attempt int, baseBackoff time.Duration) time.Duration {
	exp := max(attempt-1, 0)
	return baseBackoff * time.Duration(1<<exp)
}

// logRetry logs a retry attempt if a logger is configured
func (c *Client) logRetry(req *http.Request, attempt, maxAttempts int, err error, backoff time.Duration) {
	sleepDuration := c.calculateBackoff(attempt, backoff)
	c.logWarn("Retrying registry request",
		"method", req.Method,
		"url", req.URL.String(),
		"attempt", attempt+1,
		"max_attempts", maxAttempts,
		"reason", err.Error(),
		"backoff", sleepDuration.String(),
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
