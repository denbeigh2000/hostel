package containerd

import (
	"context"
	"strings"

	"github.com/denbeigh2000/hostel/container"

	"github.com/containerd/containerd"
	"github.com/pkg/errors"
)

/*
TODO:
 - image pull
 - periodic image updating
*/

type ContainerdSpawner struct {
	client         *containerd.Client
	containerCache map[string]struct{}
}

func New(address string) (*ContainerdSpawner, error) {
	client, err := containerd.New(address, containerd.WithDefaultNamespace("hostel"))
	if err != nil {
		return nil, errors.Wrap(err, "not able to create containerd client")
	}

	return &ContainerdSpawner{
		client:         client,
		containerCache: make(map[string]struct{}),
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
	_, err = c.client.NewContainer(ctx, id, containerd.WithImage(img))

	return nil
}

func (c *ContainerdSpawner) Spawn(ctx context.Context, taskName, containerName string, argv []string, in container.InteractiveInput) error {
	return nil
}
