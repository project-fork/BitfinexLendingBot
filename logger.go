package main

import (
	"io"
	"log"
)

func newPrefixedLogger(task string, writer io.Writer) *log.Logger {
	return log.New(writer, "["+task+"] ", log.LstdFlags|log.Lmsgprefix)
}
