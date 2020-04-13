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

type UserSessionProvider interface {
	UserSession(username string) Session
}

type Global struct {
	Host string
	Port uint32

	Defaults Session

	UserOverrides map[string]Session
}

func (g *Global) Populate() {
	if g.Host == "" {
		g.Host = DefaultHost
	}
	if g.Port == 0 {
		g.Port = DefaultPort
	}
	if g.Defaults.MaxSessionDuration == 0 {
		g.Defaults.MaxSessionDuration = DefaultMaxSessionDuration
	}
	if len(g.Defaults.DefaultArgv) == 0 {
		g.Defaults.DefaultArgv = DefaultArgv
	}
	if g.Defaults.Image == "" {
		g.Defaults.Image = DefaultImage
	}
}

func (g *Global) UserSession(username string) Session {
	userSession, ok := g.UserOverrides[username]
	if !ok {
		return g.Defaults
	}

	conf := g.Defaults
	if userSession.DefaultArgv != nil {
		conf.DefaultArgv = userSession.DefaultArgv
	}
	if userSession.Image != "" {
		conf.Image = userSession.Image
	}
	if userSession.MaxSessionDuration > 0 {
		conf.MaxSessionDuration = userSession.MaxSessionDuration
	}

	return conf
}
