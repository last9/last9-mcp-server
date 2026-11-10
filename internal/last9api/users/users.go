package users

import (
	"context"
	"net/http"

	"last9-mcp/internal/last9api"
)

const pathUsers = "/users"

type User struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	DeletedAt *int64 `json:"deleted_at,omitempty"`
	Role      string `json:"role"`
	Status    string `json:"status"`
}

// ListUsers calls GET /users.
func ListUsers(ctx context.Context, c *last9api.Client) ([]User, error) {
	var out []User
	if err := c.Do(ctx, "list users", last9api.Request{Method: http.MethodGet, Path: pathUsers}, &out); err != nil {
		return nil, err
	}
	return out, nil
}
