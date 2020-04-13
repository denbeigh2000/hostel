package container

import (
	"context"
	"io"
)

// TODO: Need to be more specific here

type Image interface{}

type TermSize struct {
	Height uint32
	Width  uint32
}

type InteractiveInput interface {
	Stdin() io.Reader
	Stdout() io.WriteCloser
	Stderr() io.Writer

	Initial() *TermSize
	Updates() <-chan TermSize
}

type ExitStatus struct {
	Code  uint32
	Error error
}

type ManagerSpawner interface {
	Manager
	Spawner
}

type Spawner interface {
	Spawn(ctx context.Context, imageRef string, argv []string, in InteractiveInput) (<-chan ExitStatus, error)
}

type Manager interface {
	Prepare(ctx context.Context, imageRef string) error
}
