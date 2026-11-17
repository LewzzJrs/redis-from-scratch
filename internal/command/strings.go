package command

import "github.com/LewzzJrs/redis-from-scratch/internal/resp"

func (r *Router) set(args []string) resp.Value {
	if len(args) != 3 {
		return syntaxError()
	}

	key, value := args[1], args[2]
	r.store.Set(key, value)
	return resp.NewSimpleString("OK")
}

func (r *Router) get(args []string) resp.Value {
	value, found := r.store.Get(args[1])
	if !found {
		return resp.NewNullBulkString()
	}
	return resp.NewBulkString(value)
}
