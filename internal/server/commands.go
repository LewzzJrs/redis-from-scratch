package server

import (
	"fmt"
	"strings"

	"github.com/LewzzJrs/redis-from-scratch/internal/resp"
)

func executeCommand(args []string) resp.Value {
	commandName := strings.ToUpper(args[0])

	switch commandName {
	case "PING":
		return executePing(args)
	default:
		return resp.NewError(unknownCommandMessage(args))
	}
}

func executePing(args []string) resp.Value {
	switch len(args) {
	case 1:
		return resp.NewSimpleString("PONG")
	case 2:
		return resp.NewBulkString(args[1])
	default:
		return wrongArgumentCountError(args[0])
	}
}

func wrongArgumentCountError(commandName string) resp.Value {
	return resp.NewError(fmt.Sprintf("ERR wrong number of arguments for '%s' command", strings.ToLower(commandName)))
}

func unknownCommandMessage(args []string) string {
	var message strings.Builder
	fmt.Fprintf(&message, "ERR unknown command '%s', with args beginning with: ", args[0])
	for _, arg := range args[1:] {
		fmt.Fprintf(&message, "'%s' ", arg)
	}
	return message.String()
}
