package containerd

import (
	"context"
	"log"
	"strings"

	"github.com/denbeigh2000/hostel/container"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
	"github.com/pkg/errors"
)

var _ container.ManagerSpawner = &ContainerdSpawner{}

type ContainerdSpawner struct {
	client         *containerd.Client
	containerCache map[string]containerd.Container
}

func New(address string) (*ContainerdSpawner, error) {
	client, err := containerd.New(address, containerd.WithDefaultNamespace("hostel"))
	if err != nil {
		return nil, errors.Wrap(err, "not able to create containerd client")
	}

	return &ContainerdSpawner{
		client:         client,
		containerCache: make(map[string]containerd.Container),
	}, nil
}

func (c *ContainerdSpawner) containerName(imageRef string) string {
	return strings.ReplaceAll(imageRef, "/", "_")
}

func (c *ContainerdSpawner) Prepare(ctx context.Context, imageRef string) error {
	img, err := c.client.Pull(ctx, imageRef)
	if err != nil {
		return errors.Wrap(err, "could not pull image with containerd")
	}

	id := c.containerName(imageRef)
	// TODO: do something with container metadata?
	container, err := c.client.NewContainer(ctx, id, containerd.WithImage(img))
	c.containerCache[id] = container

	return nil
}

func (c *ContainerdSpawner) Spawn(ctx context.Context, imageRef string, argv []string, in container.InteractiveInput) (<-chan container.ExitStatus, error) {
	key := c.containerName(imageRef)
	ctr, ok := c.containerCache[key]
	if !ok {
		err := c.Prepare(ctx, imageRef)
		if err != nil {
			return nil, errors.Wrapf(err, "could not pull image %s", imageRef)
		}

		ctr = c.containerCache[key]
	}

	stdin := in.Stdin()
	stdout := in.Stdout()
	stderr := in.Stderr()

	defer stdout.Close()

	task, err := ctr.NewTask(ctx, cio.NewCreator(cio.WithStreams(stdin, stdout, stderr)))
	if err != nil {
		return nil, errors.Wrap(err, "could not create task")
	}

	exitCh, err := task.Wait(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "could not wait for task")
	}

	ch := make(chan container.ExitStatus)
	go func() {
		defer close(ch)

		updateCh := in.Updates()

		for {
			select {
			case size := <-updateCh:
				err := task.Resize(ctx, size.Width, size.Height)
				if err != nil {
					log.Printf("could not resize: %v", err)
				}
			case status := <-exitCh:
				ch <- container.ExitStatus{
					Code:  status.ExitCode(),
					Error: status.Error(),
				}
				return
			}
		}
	}()

	return ch, nil
}
