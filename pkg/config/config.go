// Package config provides a tiny, dependency-free environment-variable
// loader shared by every service. Each service defines its own typed
// config struct in its own package and calls the helpers here to populate
// it - keeping the "what env vars does this service need" list visible in
// one place per service, rather than hidden behind a generic map.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// MustString reads a required environment variable and panics with a clear
// message if it's missing - fail fast at startup, not on the first request.
func MustString(key string) string {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		panic(fmt.Sprintf("config: required environment variable %s is not set", key))
	}
	return v
}

// String reads an optional environment variable, falling back to def.
func String(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

// Int reads an optional integer environment variable, falling back to def.
func Int(key string, def int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		panic(fmt.Sprintf("config: %s must be an integer, got %q", key, v))
	}
	return n
}

// Duration reads an optional duration environment variable (e.g. "10s",
// "5m"), falling back to def.
func Duration(key string, def time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		panic(fmt.Sprintf("config: %s must be a duration (e.g. \"10s\"), got %q", key, v))
	}
	return d
}

// Bool reads an optional boolean environment variable, falling back to def.
func Bool(key string, def bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		panic(fmt.Sprintf("config: %s must be a boolean, got %q", key, v))
	}
	return b
}
