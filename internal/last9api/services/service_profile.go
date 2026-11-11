package services

import (
	"context"
	"net/http"

	"last9-mcp/internal/last9api"
)

const pathServiceProfile = "/service_profile"

type GetServiceProfileInput struct {
	Service  string `json:"service"`
	ReadURL  string `json:"read_url"`
	Username string `json:"username"`
	Password string `json:"password"`
	Region   string `json:"region"`
}

// GetServiceProfile calls POST /service_profile.
func GetServiceProfile(ctx context.Context, c *last9api.Client, in GetServiceProfileInput) ([]byte, error) {
	in.Region = c.Region()
	var raw []byte
	err := c.Do(ctx, "service profile", last9api.Request{Method: http.MethodPost, Path: pathServiceProfile, Body: in}, &raw, last9api.WithSuccessStatus(http.StatusOK))
	return raw, err
}
