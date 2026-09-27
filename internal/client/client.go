// Package client is a small HTTP client for the Transcdr API: bearer auth, retries with backoff on
// 429 and 5xx, idempotency keys on creates, and the API's error envelope as a Go error.
package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	mrand "math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the production API.
const DefaultBaseURL = "https://api.transcdr.com"

// Client talks to one Transcdr API with one key.
type Client struct {
	BaseURL   string
	APIKey    string
	UserAgent string
	HTTP      *http.Client
	// MaxRetries is how many times a retryable request is tried again. Default 4.
	MaxRetries int
	// RetryBase is the first backoff delay; it doubles each attempt, up to RetryMax. Default 500 ms.
	RetryBase time.Duration
	// RetryMax caps a single backoff delay, including one asked for by Retry-After. Default 30 s.
	RetryMax time.Duration
}

// New returns a client with the default retry policy and a 60 s per-attempt timeout.
func New(baseURL, apiKey, userAgent string) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		APIKey:     apiKey,
		UserAgent:  userAgent,
		HTTP:       &http.Client{Timeout: 60 * time.Second},
		MaxRetries: 4,
		RetryBase:  500 * time.Millisecond,
		RetryMax:   30 * time.Second,
	}
}

// Error is an API error: the `error` envelope plus the HTTP status.
type Error struct {
	Status    int                 `json:"-"`
	Type      string              `json:"type"`
	Code      string              `json:"code"`
	Message   string              `json:"message"`
	Param     string              `json:"param"`
	Details   map[string][]string `json:"details"`
	RequestID string              `json:"request_id"`
	Method    string              `json:"-"`
	Path      string              `json:"-"`
}

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s: %d", e.Method, e.Path, e.Status)
	if e.Code != "" {
		fmt.Fprintf(&b, " %s", e.Code)
	} else if e.Type != "" {
		fmt.Fprintf(&b, " %s", e.Type)
	}
	if e.Message != "" {
		fmt.Fprintf(&b, ": %s", e.Message)
	}
	return b.String()
}

// Detail is the multi-line explanation for a diagnostic: the message, each field error, and the
// request id to quote to support.
func (e *Error) Detail() string {
	var b strings.Builder
	b.WriteString(e.Message)
	if e.Message == "" {
		fmt.Fprintf(&b, "The API answered %d.", e.Status)
	}
	if len(e.Details) > 0 {
		b.WriteString("\n\nField errors:")
		for _, field := range sortedKeys(e.Details) {
			for _, msg := range e.Details[field] {
				fmt.Fprintf(&b, "\n  - %s: %s", field, msg)
			}
		}
	} else if e.Param != "" {
		fmt.Fprintf(&b, "\n\nField: %s", e.Param)
	}
	fmt.Fprintf(&b, "\n\n%s %s returned HTTP %d", e.Method, e.Path, e.Status)
	if e.Code != "" {
		fmt.Fprintf(&b, " (%s: %s)", e.Type, e.Code)
	} else if e.Type != "" {
		fmt.Fprintf(&b, " (%s)", e.Type)
	}
	b.WriteString(".")
	if e.RequestID != "" {
		fmt.Fprintf(&b, " Request id: %s.", e.RequestID)
	}
	return b.String()
}

// IsNotFound reports whether err is a 404 from the API.
func IsNotFound(err error) bool {
	var apiErr *Error
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}

// IsConflict reports whether err is a 409 from the API.
func IsConflict(err error) bool {
	var apiErr *Error
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict
}

// RequestOptions tune one call.
type RequestOptions struct {
	// IdempotencyKey is sent as `Idempotency-Key`; Create sets a random one.
	IdempotencyKey string
	Query          url.Values
}

// Get fetches path into out.
func (c *Client) Get(ctx context.Context, path string, query url.Values, out any) error {
	return c.Do(ctx, http.MethodGet, path, nil, out, RequestOptions{Query: query})
}

// Create POSTs body with a fresh idempotency key.
func (c *Client) Create(ctx context.Context, path string, body, out any) error {
	return c.Do(ctx, http.MethodPost, path, body, out, RequestOptions{IdempotencyKey: NewIdempotencyKey()})
}

// Post sends an action that is not a create (e.g. rotate a secret).
func (c *Client) Post(ctx context.Context, path string, body, out any) error {
	return c.Do(ctx, http.MethodPost, path, body, out, RequestOptions{})
}

// Patch sends a partial update.
func (c *Client) Patch(ctx context.Context, path string, body, out any) error {
	return c.Do(ctx, http.MethodPatch, path, body, out, RequestOptions{})
}

// Delete removes path. A 404 is not an error: the object is already gone.
func (c *Client) Delete(ctx context.Context, path string) error {
	err := c.Do(ctx, http.MethodDelete, path, nil, nil, RequestOptions{})
	if IsNotFound(err) {
		return nil
	}
	return err
}

// retryable reports whether a request may be sent again after a failure. GET, DELETE and the
// provider's PATCHes (which always carry absolute values) are safe to repeat. A POST is repeated
// only when the API refused it outright with 429, since the API does not deduplicate creates by
// idempotency key everywhere.
func retryable(method string, status int) bool {
	switch method {
	case http.MethodGet, http.MethodDelete, http.MethodPatch, http.MethodPut, http.MethodHead:
		return status == 0 || status == http.StatusTooManyRequests || status >= 500
	default:
		return status == http.StatusTooManyRequests
	}
}

// Do sends one request, retrying as the policy allows, and decodes a 2xx JSON body into out.
func (c *Client) Do(ctx context.Context, method, path string, body, out any, opts RequestOptions) error {
	target := c.BaseURL + path
	if len(opts.Query) > 0 {
		target += "?" + opts.Query.Encode()
	}
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return fmt.Errorf("encoding the request body: %w", err)
		}
	}

	for attempt := 0; ; attempt++ {
		var reader io.Reader
		if payload != nil {
			reader = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, target, reader)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/json")
		if c.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.APIKey)
		}
		if c.UserAgent != "" {
			req.Header.Set("User-Agent", c.UserAgent)
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if opts.IdempotencyKey != "" {
			req.Header.Set("Idempotency-Key", opts.IdempotencyKey)
		}

		resp, err := c.HTTP.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if attempt < c.MaxRetries && retryable(method, 0) {
				if werr := c.wait(ctx, attempt, ""); werr != nil {
					return werr
				}
				continue
			}
			return fmt.Errorf("could not reach the Transcdr API at %s: %w", c.BaseURL, err)
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if readErr != nil {
				return fmt.Errorf("reading the response of %s %s: %w", method, path, readErr)
			}
			if out == nil || len(bytes.TrimSpace(data)) == 0 {
				return nil
			}
			if err := json.Unmarshal(data, out); err != nil {
				return fmt.Errorf("decoding the response of %s %s: %w", method, path, err)
			}
			return nil
		}

		if attempt < c.MaxRetries && retryable(method, resp.StatusCode) {
			if werr := c.wait(ctx, attempt, resp.Header.Get("Retry-After")); werr != nil {
				return werr
			}
			continue
		}
		return parseError(method, path, resp, data)
	}
}

func parseError(method, path string, resp *http.Response, data []byte) error {
	var envelope struct {
		Error *Error `json:"error"`
	}
	apiErr := &Error{}
	if json.Unmarshal(data, &envelope) == nil && envelope.Error != nil {
		apiErr = envelope.Error
	} else if text := strings.TrimSpace(string(data)); text != "" {
		if len(text) > 500 {
			text = text[:500]
		}
		apiErr.Message = text
	}
	apiErr.Status = resp.StatusCode
	apiErr.Method = method
	apiErr.Path = path
	if apiErr.RequestID == "" {
		apiErr.RequestID = resp.Header.Get("X-Request-Id")
	}
	if apiErr.Type == "" {
		apiErr.Type = defaultType(resp.StatusCode)
	}
	return apiErr
}

func defaultType(status int) string {
	switch {
	case status == 401:
		return "authentication_error"
	case status == 402:
		return "quota_error"
	case status == 403:
		return "permission_error"
	case status == 429:
		return "rate_limit_error"
	case status >= 500:
		return "api_error"
	default:
		return "invalid_request_error"
	}
}

// Backoff is the delay before retry number attempt+1: Retry-After when the API sent one, else
// exponential with full jitter in [d/2, d].
func (c *Client) Backoff(attempt int, retryAfter string) time.Duration {
	limit := c.RetryMax
	if limit <= 0 {
		limit = 30 * time.Second
	}
	if retryAfter != "" {
		if seconds, err := strconv.ParseFloat(retryAfter, 64); err == nil && seconds >= 0 {
			return min(time.Duration(seconds*float64(time.Second)), limit)
		}
		if at, err := http.ParseTime(retryAfter); err == nil {
			return max(0, min(time.Until(at), limit))
		}
	}
	base := c.RetryBase
	if base <= 0 {
		base = 500 * time.Millisecond
	}
	d := time.Duration(math.Min(float64(base)*math.Pow(2, float64(attempt)), float64(limit)))
	return d/2 + time.Duration(mrand.Int64N(int64(d/2)+1))
}

func (c *Client) wait(ctx context.Context, attempt int, retryAfter string) error {
	timer := time.NewTimer(c.Backoff(attempt, retryAfter))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// NewIdempotencyKey is a random 128-bit hex key.
func NewIdempotencyKey() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return "tf-" + hex.EncodeToString(b[:])
}

// PathEscape escapes one path segment (an id or slug).
func PathEscape(segment string) string {
	return url.PathEscape(segment)
}

// List is one page of a cursor-paginated list.
type List[T any] struct {
	Data       []T    `json:"data"`
	HasMore    bool   `json:"has_more"`
	NextCursor string `json:"next_cursor"`
}

// ListAll follows next_cursor until the last page, or until stop returns true for an item.
func ListAll[T any](ctx context.Context, c *Client, path string, query url.Values, stop func(T) bool) ([]T, error) {
	var all []T
	q := url.Values{}
	for k, v := range query {
		q[k] = v
	}
	q.Set("limit", "100")
	for {
		var page List[T]
		if err := c.Get(ctx, path, q, &page); err != nil {
			return nil, err
		}
		for _, item := range page.Data {
			all = append(all, item)
			if stop != nil && stop(item) {
				return all, nil
			}
		}
		if !page.HasMore || page.NextCursor == "" {
			return all, nil
		}
		q.Set("cursor", page.NextCursor)
	}
}

func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}
