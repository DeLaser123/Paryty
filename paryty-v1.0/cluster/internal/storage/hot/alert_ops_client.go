// Adapter that binds AlertOps to a live Dragonfly client.
//
// AlertOps depends on the narrow redisCmdable interface whose Pipeline()
// method returns the alertPipeline subset. *redis.Client's Pipeline()
// returns redis.Pipeliner, so the concrete client does not structurally
// satisfy redisCmdable — this thin adapter bridges the two.
package hot

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// clientCmdable adapts *redis.Client to the redisCmdable interface used by
// AlertOps.
type clientCmdable struct {
	rdb *redis.Client
}

func (c clientCmdable) Get(ctx context.Context, key string) *redis.StringCmd {
	return c.rdb.Get(ctx, key)
}

func (c clientCmdable) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	return c.rdb.Set(ctx, key, value, expiration)
}

func (c clientCmdable) SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.BoolCmd {
	return c.rdb.SetNX(ctx, key, value, expiration)
}

func (c clientCmdable) Del(ctx context.Context, keys ...string) *redis.IntCmd {
	return c.rdb.Del(ctx, keys...)
}

func (c clientCmdable) Scan(ctx context.Context, cursor uint64, match string, count int64) *redis.ScanCmd {
	return c.rdb.Scan(ctx, cursor, match, count)
}

func (c clientCmdable) Pipeline() alertPipeline {
	return c.rdb.Pipeline()
}

// AlertOps returns an AlertOps bound to this client's Dragonfly connection.
// A nil logger is replaced with zap.NewNop().
func (c *Client) AlertOps(logger *zap.Logger) *AlertOps {
	if logger == nil {
		logger = zap.NewNop()
	}
	return NewAlertOps(clientCmdable{rdb: c.rdb}, logger)
}
