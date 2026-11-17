package command

import "github.com/LewzzJrs/redis-from-scratch/internal/resp"

func (r *Router) dbsize(args []string) resp.Value {
	return resp.NewInteger(int64(r.store.Len()))
}
