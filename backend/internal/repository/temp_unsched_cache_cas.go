package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

var tempUnschedDeleteCAS = redis.NewScript(`
 local value = redis.call('GET', KEYS[1])
 if not value then return 1 end
 if value ~= ARGV[1] then return 0 end
 redis.call('DEL', KEYS[1])
 return 1
`)

func (c *tempUnschedCache) DeleteTempUnschedIfUnchanged(ctx context.Context, id int64, expected *service.TempUnschedState) (bool, error) {
	if id <= 0 || expected == nil {
		return false, nil
	}
	raw, err := json.Marshal(expected)
	if err != nil {
		return false, err
	}
	changed, err := tempUnschedDeleteCAS.Run(ctx, c.rdb, []string{fmt.Sprintf("%s%d", tempUnschedPrefix, id)}, string(raw)).Int64()
	return changed == 1, err
}
