package command

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/LewzzJrs/redis-from-scratch/internal/resp"
)

var (
	errSyntax            = errors.New("ERR syntax error")
	errNotAnInteger      = errors.New("ERR value is not an integer or out of range")
	errInvalidExpireTime = errors.New("ERR invalid expire time in 'set' command")
)

func (r *Router) set(args []string) resp.Value {
	key, value := args[1], args[2]

	timeToLive, err := parseSetOptions(args[3:])
	if err != nil {
		return resp.NewError(err.Error())
	}

	if timeToLive > 0 {
		r.store.SetWithExpiry(key, value, timeToLive)
	} else {
		r.store.Set(key, value)
	}
	return resp.NewSimpleString("OK")
}

func (r *Router) get(args []string) resp.Value {
	value, found := r.store.Get(args[1])
	if !found {
		return resp.NewNullBulkString()
	}
	return resp.NewBulkString(value)
}

func parseSetOptions(options []string) (time.Duration, error) {
	var timeToLive time.Duration

	for index := 0; index < len(options); index++ {
		var unit time.Duration
		switch strings.ToUpper(options[index]) {
		case "EX":
			unit = time.Second
		case "PX":
			unit = time.Millisecond
		default:
			return 0, errSyntax
		}

		hasAmount := index+1 < len(options)
		if timeToLive > 0 || !hasAmount {
			return 0, errSyntax
		}

		index++
		parsedTimeToLive, err := parseExpireAmount(options[index], unit)
		if err != nil {
			return 0, err
		}
		timeToLive = parsedTimeToLive
	}

	return timeToLive, nil
}

func parseExpireAmount(amountText string, unit time.Duration) (time.Duration, error) {
	amount, err := strconv.ParseInt(amountText, 10, 64)
	if err != nil {
		return 0, errNotAnInteger
	}

	maxAmountForUnit := int64(math.MaxInt64 / unit)
	if amount <= 0 || amount > maxAmountForUnit {
		return 0, errInvalidExpireTime
	}
	return time.Duration(amount) * unit, nil
}
