package command

import "github.com/LewzzJrs/redis-from-scratch/internal/resp"

func (r *Router) ping(args []string) resp.Value {
	switch len(args) {
	case 1:
		return resp.NewSimpleString("PONG")
	case 2:
		return resp.NewBulkString(args[1])
	default:
		return wrongArgumentCountError(args[0])
	}
}

func (r *Router) echo(args []string) resp.Value {
	return resp.NewBulkString(args[1])
}
