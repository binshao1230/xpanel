package master

import "context"

type ctxKey int

const userKey ctxKey = 1

func withUser(ctx context.Context, c *claims) context.Context {
	return context.WithValue(ctx, userKey, c)
}

func userFrom(ctx context.Context) *claims {
	v, _ := ctx.Value(userKey).(*claims)
	return v
}
