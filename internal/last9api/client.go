// Package last9api is the single door for HTTP calls to the Last9 API.
package last9api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
)

type Client struct {
	http *http.Client
	cfg  models.Config
}

func NewClient(httpClient *http.Client, cfg models.Config) *Client {
	return &Client{http: httpClient, cfg: cfg}
}

func (c *Client) ClusterID() string { return c.cfg.ClusterID }

func (c *Client) Region() string { return c.cfg.Region }

type Request struct {
	Method string
	Path   string
	Body   any
}

type options struct {
	query        url.Values
	regionHeader bool
	regionQuery  bool
}

type Option func(*options)

func WithQuery(q url.Values) Option { return func(o *options) { o.query = q } }

func WithRegionHeader() Option { return func(o *options) { o.regionHeader = true } }

func WithRegionQuery() Option { return func(o *options) { o.regionQuery = true } }

func (c *Client) Do(ctx context.Context, op string, r Request, out any, opts ...Option) error {
	o := applyOptions(opts)

	token, err := c.resolveToken(ctx, o)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	req, err := c.newRequest(ctx, r, o, token)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	defer resp.Body.Close()

	if !isSuccess(resp.StatusCode) {
		return utils.NewUpstreamHTTPError(resp, op)
	}
	return decodeResponse(resp, op, out)
}

func applyOptions(opts []Option) options {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// Runs before any I/O so a misconfiguration reads as a clear error rather than an
// upstream 400. GetAccessToken returns a bare string, so a failed refresh surfaces as ""
// and must never be sent as "Bearer ".
func (c *Client) resolveToken(ctx context.Context, o options) (string, error) {
	if err := c.validateConfig(o); err != nil {
		return "", err
	}
	token := strings.TrimSpace(c.cfg.TokenManager.GetAccessToken(ctx))
	if token == "" {
		return "", errors.New("access token is empty")
	}
	return token, nil
}

func (c *Client) validateConfig(o options) error {
	if c.http == nil {
		return errors.New("http client is nil")
	}
	if strings.TrimSpace(c.cfg.APIBaseURL) == "" {
		return errors.New("API base URL is empty")
	}
	if c.cfg.TokenManager == nil {
		return errors.New("token manager is nil")
	}
	if (o.regionHeader || o.regionQuery) && strings.TrimSpace(c.cfg.Region) == "" {
		return errors.New("region is required for this endpoint but is empty")
	}
	return nil
}

func (c *Client) newRequest(
	ctx context.Context, r Request, o options, token string,
) (*http.Request, error) {
	body, err := encodeBody(r.Body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, c.buildURL(r.Path, o), body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	setHeaders(req, token, r.Body != nil)
	if o.regionHeader {
		req.Header.Set(headerRegion, c.cfg.Region)
	}
	return req, nil
}

func encodeBody(body any) (io.Reader, error) {
	if body == nil {
		return nil, nil
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	return bytes.NewReader(raw), nil
}

func setHeaders(req *http.Request, token string, hasBody bool) {
	req.Header.Set(headerAccept, contentTypeJSON)
	req.Header.Set(headerUserAgent, userAgent)
	req.Header.Set(headerAPIToken, bearerPrefix+token)
	if hasBody {
		req.Header.Set(headerContentType, contentTypeJSON)
	}
}

// The path is never normalized: some collection routes require a trailing slash and the
// API does not redirect between the two forms.
func (c *Client) buildURL(path string, o options) string {
	full := c.cfg.APIBaseURL + path
	q := url.Values{}
	for k, vs := range o.query {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	if o.regionQuery {
		q.Set(regionKey, c.cfg.Region)
	}
	if len(q) == 0 {
		return full
	}
	return full + "?" + q.Encode()
}

func isSuccess(status int) bool {
	return status >= http.StatusOK && status < http.StatusMultipleChoices
}

func decodeResponse(resp *http.Response, op string, out any) error {
	if out == nil {
		utils.DrainResponseBody(resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s: decode response: %w", op, err)
	}
	return nil
}
