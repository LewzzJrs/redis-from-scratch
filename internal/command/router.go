package command

import (
	"fmt"
	"strings"

	"github.com/LewzzJrs/redis-from-scratch/internal/resp"
	"github.com/LewzzJrs/redis-from-scratch/internal/store"
)

type handlerFunc func(args []string) resp.Value

type commandSpec struct {
	arity   int
	handler handlerFunc
}

type Router struct {
	store    *store.Store
	commands map[string]commandSpec
}

func NewRouter(dataStore *store.Store) *Router {
	router := &Router{store: dataStore}
	router.commands = map[string]commandSpec{
		"PING": {arity: -1, handler: router.ping},
		"ECHO": {arity: 2, handler: router.echo},
		"SET":  {arity: -3, handler: router.set},
		"GET":  {arity: 2, handler: router.get},
	}
	return router
}

func (r *Router) Execute(args []string) resp.Value {
	if len(args) == 0 {
		return resp.NewError("ERR empty command")
	}

	spec, known := r.commands[strings.ToUpper(args[0])]
	if !known {
		return unknownCommandError(args)
	}
	if !hasValidArity(spec.arity, len(args)) {
		return wrongArgumentCountError(args[0])
	}
	return spec.handler(args)
}

func hasValidArity(arity int, argumentCount int) bool {
	if arity >= 0 {
		return argumentCount == arity
	}
	return argumentCount >= -arity
}

func unknownCommandError(args []string) resp.Value {
	var message strings.Builder
	fmt.Fprintf(&message, "ERR unknown command '%s', with args beginning with: ", args[0])
	for _, arg := range args[1:] {
		fmt.Fprintf(&message, "'%s' ", arg)
	}
	return resp.NewError(message.String())
}

func wrongArgumentCountError(commandName string) resp.Value {
	return resp.NewError(fmt.Sprintf("ERR wrong number of arguments for '%s' command", strings.ToLower(commandName)))
}

func syntaxError() resp.Value {
	return resp.NewError("ERR syntax error")
}
