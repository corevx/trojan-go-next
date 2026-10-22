package common

import (
	"fmt"
)

type Error struct {
	info  string
	cause error
}

func (e *Error) Error() string {
	return e.info
}

func (e *Error) Base(err error) *Error {
	if err != nil {
		e.info += " | " + err.Error()
		e.cause = err
	}
	return e
}

// Unwrap exposes the error passed to Base so errors.Is/As can see through
// the chain. Error()'s output format stays unchanged ("a | b | c").
func (e *Error) Unwrap() error {
	return e.cause
}

func NewError(info string) *Error {
	return &Error{
		info: info,
	}
}

func Must(err error) {
	if err != nil {
		fmt.Println(err)
		panic(err)
	}
}

func Must2(_ interface{}, err error) {
	if err != nil {
		fmt.Println(err)
		panic(err)
	}
}
