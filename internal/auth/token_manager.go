package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"last9-mcp/internal/constants"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
)

var (
	httpClient     *http.Client
	httpClientOnce sync.Once
)

type TokenManager struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time

	// Synchronization
	mu          sync.RWMutex
	refreshing  bool
	refreshCond *sync.Cond

	// Configuration
	refreshBuffer time.Duration
}

// ExtractOrgSlugFromToken extracts organization slug from JWT token
func ExtractOrgSlugFromToken(accessToken string) (string, error) {
	claims, err := ExtractClaimsFromToken(accessToken)
	if err != nil {
		return "", fmt.Errorf("failed to extract claims from token: %w", err)
	}

	orgSlug, ok := claims["organization_slug"].(string)
	if !ok {
		return "", errors.New("organization slug not found in token")
	}

	return orgSlug, nil
}

func ExtractClaimsFromToken(accessToken string) (map[string]interface{}, error) {
	// Split the token into parts
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid JWT token format")
	}

	// Decode the payload (second part)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode token payload: %w", err)
	}

	// Parse the JSON payload
	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("failed to parse token claims: %w", err)
	}

	return claims, nil
}

func ExtractActionURLFromToken(accessToken string) (string, error) {
	// Extract ActionURL from token claims
	claims, err := ExtractClaimsFromToken(accessToken)
	if err != nil {
		return "", fmt.Errorf("failed to extract claims from token: %w", err)
	}

	// Get ActionURL from aud field
	aud, ok := claims["aud"].([]interface{})
	if !ok || len(aud) == 0 {
		return "", errors.New("no audience found in token claims")
	}

	// Handle case where audience already includes protocol
	audStr := aud[0].(string)
	if strings.HasPrefix(audStr, "https://") || strings.HasPrefix(audStr, "http://") {
		return audStr, nil
	}
	// Default to HTTPS if no protocol specified
	return fmt.Sprintf("https://%s", audStr), nil
}

// RefreshAccessToken gets a new access token using the refresh token
func RefreshAccessToken(ctx context.Context, client *http.Client, refreshToken string) (string, error) {
	data := map[string]string{
		"refresh_token": refreshToken,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}
	// Extract ActionURL from token claims
	actionURL, err := ExtractActionURLFromToken(refreshToken)
	if err != nil {
		return "", fmt.Errorf("failed to extract action URL from refresh token: %w", err)
	}

	// Handle case where actionURL already includes /api path
	oauthURL := actionURL
	if strings.HasSuffix(actionURL, "/api") {
		oauthURL = strings.TrimSuffix(actionURL, "/api")
	}
	u, err := url.Parse(oauthURL + "/api/v4/oauth/access_token")
	if err != nil {
		return "", fmt.Errorf("failed to parse URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewReader(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return result.AccessToken, nil
}

func NewTokenManager(refreshToken string) (*TokenManager, error) {
	client := GetHTTPClient()
	accessToken, err := RefreshAccessToken(context.Background(), client, refreshToken)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain initial access token: %w", err)
	}

	expiry, err := GetTokenExpiry(accessToken)
	if err != nil {
		return nil, fmt.Errorf("failed to parse access token expiry: %w", err)
	}

	tm := &TokenManager{
		AccessToken:   accessToken,
		RefreshToken:  refreshToken,
		ExpiresAt:     expiry,
		refreshBuffer: expiry.Sub(time.Now()) / 2, // 50% of token lifespan
	}
	tm.refreshCond = sync.NewCond(&tm.mu)

	// background refresh goroutine
	go tm.backgroundRefresh()

	return tm, nil
}

// GetTokenExpiry extracts the expiration time from a JWT access token
func GetTokenExpiry(accessToken string) (time.Time, error) {
	claims, err := ExtractClaimsFromToken(accessToken)
	if err != nil {
		return time.Time{}, err
	}

	// Extract exp claim
	exp, ok := claims["exp"].(float64)
	if !ok {
		return time.Time{}, errors.New("no expiration time found in token")
	}

	return time.Unix(int64(exp), 0), nil
}

func (tm *TokenManager) GetAccessToken(ctx context.Context) string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	// Check if token is valid
	if time.Now().Before(tm.ExpiresAt.Add(-tm.refreshBuffer)) {
		return tm.AccessToken
	}

	if !tm.refreshing {
		go tm.refreshToken(ctx)
	}

	for tm.refreshing {
		tm.refreshCond.Wait()
	}

	return tm.AccessToken
}

func (tm *TokenManager) refreshToken(ctx context.Context) {
	tm.mu.Lock()
	if tm.refreshing {
		tm.mu.Unlock()
		return
	}
	tm.refreshing = true
	tm.mu.Unlock()

	defer func() {
		tm.mu.Lock()
		tm.refreshing = false
		tm.refreshCond.Broadcast()
		tm.mu.Unlock()
	}()

	client := GetHTTPClient()
	newAccessToken, err := RefreshAccessToken(ctx, client, tm.RefreshToken)
	if err != nil {
		return
	}

	expiry, err := GetTokenExpiry(newAccessToken)
	if err != nil {
		return
	}

	tm.mu.Lock()
	tm.AccessToken = newAccessToken
	tm.ExpiresAt = expiry
	tm.mu.Unlock()
}

func (tm *TokenManager) backgroundRefresh() {
	ticker := time.NewTicker(45 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		tm.mu.RLock()
		needsRefresh := time.Now().After(tm.ExpiresAt.Add(-tm.refreshBuffer))
		tm.mu.RUnlock()

		if needsRefresh {
			tm.refreshToken(context.Background())
		}
	}
}

func GetHTTPClient() *http.Client {
	// implement sync.Once if needed in future
	httpClientOnce.Do(func() {
		httpClient = last9mcp.WithHTTPTracing(&http.Client{
			Timeout: 30 * time.Second,
		})
	})

	return httpClient
}
