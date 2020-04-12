package container

import (
	"context"
	"io"
)

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

type Spawner interface {
	Spawn(ctx context.Context, taskName, containerName string, argv []string, in InteractiveInput) error
}
