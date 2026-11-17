package command

import (
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/LewzzJrs/redis-from-scratch/internal/resp"
	"github.com/LewzzJrs/redis-from-scratch/internal/store"
)

type CommandLog interface {
	Append(args ...string) error
}

type discardLog struct{}

func (discardLog) Append(args ...string) error {
	return nil
}

type handlerFunc func(args []string) resp.Value

type commandSpec struct {
	arity      int
	writesData bool
	handler    handlerFunc
}

type Router struct {
	store      *store.Store
	commandLog CommandLog
	commands   map[string]commandSpec
	writeOrder sync.Mutex
}

func NewRouter(dataStore *store.Store, commandLog CommandLog) *Router {
	if commandLog == nil {
		commandLog = discardLog{}
	}

	router := &Router{store: dataStore, commandLog: commandLog}
	router.commands = map[string]commandSpec{
		"PING":   {arity: -1, handler: router.ping},
		"ECHO":   {arity: 2, handler: router.echo},
		"SET":    {arity: -3, writesData: true, handler: router.set},
		"GET":    {arity: 2, handler: router.get},
		"INCR":   {arity: 2, writesData: true, handler: router.incr},
		"DEL":    {arity: -2, writesData: true, handler: router.del},
		"EXISTS": {arity: -2, handler: router.exists},
		"KEYS":   {arity: 2, handler: router.keys},
		"DBSIZE": {arity: 1, handler: router.dbsize},
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

	if spec.writesData {
		r.writeOrder.Lock()
		defer r.writeOrder.Unlock()
	}
	return spec.handler(args)
}

func (r *Router) persist(args ...string) {
	if err := r.commandLog.Append(args...); err != nil {
		log.Printf("failed to append %s to command log: %v", args[0], err)
	}
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
