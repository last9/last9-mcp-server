package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"last9-mcp/internal/utils"
)

type TokenManager struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time

	// Synchronization
	mu          sync.RWMutex
	resfreshing bool
	refreshCond *sync.Cond

	// Configuration
	refreshBuffer time.Duration

	// Resilience
	refreshCircuit *RefreshCircuitBreaker
}

func NewTokenManager(accessToken, refreshToken string) (*TokenManager, error) {
	// find expiry time from access token
	expiry, err := GetTokenExpiry(accessToken)
	if err != nil {
		return nil, fmt.Errorf("failed to get token expiry: %v", err)
	}

	tm := &TokenManager{
		AccessToken:   accessToken,
		RefreshToken:  refreshToken,
		ExpiresAt:     expiry,
		refreshBuffer: expiry.Sub(time.Now()) / 2, // 50% of token lifespan

		refeshCircuit: NewRefreshCircuitBreaker(3, 1*time.Minute),
	}
	tm.refreshCond = sync.NewCond(&tm.mu)

	// background refresh goroutine
	go tm.backgroundRefresh()

	return tm, nil
}

// GetTokenExpiry extracts the expiration time from a JWT access token
func GetTokenExpiry(accessToken string) (time.Time, error) {
	claims, err := utils.ExtractClaimsFromToken(accessToken)
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

func (tm *TokenManger) GetAccessToken(ctx context.Context) (string, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	// Check if token is valid
	if time.Now().Before(tm.ExpiresAt.Add(-tm.refreshBuffer)) {
		return tm.AccessToken, nil
	}

	if !tm.refreshing {
		go tm.refreshToken(ctx)
	}

	for tm.refeshing {
		tm.refreshCond.Wait()
	}

	// Check again after wait
	if time.Now().Before(tm.ExpiresAt.Add(-tm.refreshBuffer)) {
		return tm.AccessToken, nil
	}

	return "", errors.New("token refresh failed")
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

	// Check circuit breaker
	if !tm.refreshCircuit.Allow() {
		return
	}

	newAccessToken, newRefreshToken, err := utils.RefreshTokens(ctx, tm.RefreshToken)
	if err != nil {
		tm.refreshCircuit.RecordFailure()
		return
	}

	expiry, err := GetTokenExpiry(newAccessToken)
	if err != nil {
		tm.refreshCircuit.RecordFailure()
		return
	}

	tm.mu.Lock()
	tm.AccessToken = newAccessToken
	tm.RefreshToken = newRefreshToken
	tm.ExpiresAt = expiry
	tm.mu.Unlock()

	tm.refreshCircuit.RecordSuccess()
}

