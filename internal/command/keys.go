package command

import (
	"github.com/LewzzJrs/redis-from-scratch/internal/glob"
	"github.com/LewzzJrs/redis-from-scratch/internal/resp"
)

func (r *Router) del(args []string) resp.Value {
	deletedCount := r.store.Delete(args[1:]...)
	return resp.NewInteger(int64(deletedCount))
}

func (r *Router) exists(args []string) resp.Value {
	existingCount := 0
	for _, key := range args[1:] {
		if r.store.Exists(key) {
			existingCount++
		}
	}
	return resp.NewInteger(int64(existingCount))
}

func (r *Router) keys(args []string) resp.Value {
	pattern := args[1]
	matchingKeys := r.store.Keys(func(key string) bool {
		return glob.Match(pattern, key)
	})

	items := make([]resp.Value, len(matchingKeys))
	for index, key := range matchingKeys {
		items[index] = resp.NewBulkString(key)
	}
	return resp.NewArray(items...)
}

func (r *Router) dbsize(args []string) resp.Value {
	return resp.NewInteger(int64(r.store.Len()))
}
