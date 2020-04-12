package config

import (
	"time"
)

const (
	DefaultImage              = "docker.io/library/ubuntu:latest"
	DefaultShell              = "/bin/bash"
	DefaultMaxSessionDuration = 24 * time.Hour

	DefaultHost = "127.0.0.1"
	DefaultPort = 2022
)

var DefaultArgv = []string{DefaultShell}

type Session struct {
	DefaultArgv        []string
	Image              string
	MaxSessionDuration time.Duration
}

type Global struct {
	Host string
	Port uint32

	Defaults Session

	UserOverrides map[string]Session
}
