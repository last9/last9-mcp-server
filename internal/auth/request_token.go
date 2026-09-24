package auth

import "context"

type requestTokenKey struct{}

func WithRequestToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, requestTokenKey{}, token)
}

func RequestTokenFromContext(ctx context.Context) (string, bool) {
	token, ok := ctx.Value(requestTokenKey{}).(string)
	return token, ok && token != ""
}
