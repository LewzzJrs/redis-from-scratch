package command

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/LewzzJrs/redis-from-scratch/internal/resp"
	"github.com/LewzzJrs/redis-from-scratch/internal/store"
)

var (
	errSyntax            = errors.New("ERR syntax error")
	errNotAnInteger      = errors.New("ERR value is not an integer or out of range")
	errInvalidExpireTime = errors.New("ERR invalid expire time in 'set' command")
	errIncrementOverflow = errors.New("ERR increment or decrement would overflow")
)

type expiryOption struct {
	unit       time.Duration
	isAbsolute bool
}

var expiryOptions = map[string]expiryOption{
	"EX":   {unit: time.Second},
	"PX":   {unit: time.Millisecond},
	"EXAT": {unit: time.Second, isAbsolute: true},
	"PXAT": {unit: time.Millisecond, isAbsolute: true},
}

func (r *Router) set(args []string) resp.Value {
	key, value := args[1], args[2]

	deadline, err := parseSetOptions(args[3:], r.store.Now())
	if err != nil {
		return resp.NewError(err.Error())
	}

	if deadline.IsZero() {
		r.store.Set(key, value)
	} else {
		r.store.SetWithDeadline(key, value, deadline)
	}
	r.persistSet(key, value, deadline)
	return resp.NewSimpleString("OK")
}

func (r *Router) get(args []string) resp.Value {
	value, found := r.store.Get(args[1])
	if !found {
		return resp.NewNullBulkString()
	}
	return resp.NewBulkString(value)
}

func (r *Router) incr(args []string) resp.Value {
	key := args[1]

	newNumber, expiresAt, err := r.store.Increment(key, 1)
	switch {
	case errors.Is(err, store.ErrNotInteger):
		return resp.NewError(errNotAnInteger.Error())
	case errors.Is(err, store.ErrIncrementOverflow):
		return resp.NewError(errIncrementOverflow.Error())
	case err != nil:
		return resp.NewError("ERR " + err.Error())
	}

	r.persistSet(key, strconv.FormatInt(newNumber, 10), expiresAt)
	return resp.NewInteger(newNumber)
}

func (r *Router) persistSet(key string, value string, deadline time.Time) {
	if deadline.IsZero() {
		r.persist("SET", key, value)
		return
	}
	r.persist("SET", key, value, "PXAT", strconv.FormatInt(deadline.UnixMilli(), 10))
}

func parseSetOptions(options []string, now time.Time) (time.Time, error) {
	var deadline time.Time

	for index := 0; index < len(options); index++ {
		option, known := expiryOptions[strings.ToUpper(options[index])]
		if !known {
			return time.Time{}, errSyntax
		}

		hasAmount := index+1 < len(options)
		if !deadline.IsZero() || !hasAmount {
			return time.Time{}, errSyntax
		}

		index++
		amount, err := parseExpireAmount(options[index], option.unit)
		if err != nil {
			return time.Time{}, err
		}

		if option.isAbsolute {
			deadline = time.Unix(0, 0).Add(amount)
		} else {
			deadline = now.Add(amount)
		}
	}

	return deadline, nil
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
