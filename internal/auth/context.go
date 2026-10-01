package auth

import (
	"context"

	"github.com/example/game-server-control-plane/internal/model"
)

type principalKey struct{}

func WithPrincipal(ctx context.Context, principal model.Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, principal)
}

func PrincipalFromContext(ctx context.Context) (model.Principal, bool) {
	principal, ok := ctx.Value(principalKey{}).(model.Principal)
	return principal, ok
}
