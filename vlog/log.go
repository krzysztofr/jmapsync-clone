// Copyright 2025 Daniel Erat.
// All rights reserved.

// Package vlog writes messages to a logger attached to a context.
package vlog

import (
	"context"
	"fmt"
	"log"
)

type keyType string

var key = keyType("log")

// LoggerContext attaches lg to a new context derived from ctx.
// The returned context can later be passed to Log or Logf.
func LoggerContext(ctx context.Context, lg *log.Logger) context.Context {
	return context.WithValue(ctx, key, lg)
}

// Log writes args to the log.Logger previously attached to ctx.
// If no logger is attached, writes nothing.
func Log(ctx context.Context, args ...any) {
	if lg, ok := ctx.Value(key).(*log.Logger); ok {
		lg.Print(fmt.Sprint(args...))
	}
}

// Logf is like Log but uses fmt.Sprintf.
func Logf(ctx context.Context, format string, args ...any) {
	Log(ctx, fmt.Sprintf(format, args...))
}
