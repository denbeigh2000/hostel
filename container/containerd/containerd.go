package containerd

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"syscall"
	"time"

	"github.com/denbeigh2000/hostel/container"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/oci"
	"github.com/pkg/errors"
)

const hostelNamespace = "hostel"

var _ container.ManagerSpawner = &ContainerdSpawner{}

var sanitiseContainerRE = regexp.MustCompile(":|/")

type ContainerdSpawner struct {
	client     *containerd.Client
	imageCache map[string]containerd.Image
}

func New(address string) (*ContainerdSpawner, error) {
	client, err := containerd.New(address, containerd.WithDefaultNamespace(hostelNamespace))
	if err != nil {
		return nil, errors.Wrap(err, "not able to create containerd client")
	}

	return &ContainerdSpawner{
		client:     client,
		imageCache: make(map[string]containerd.Image),
	}, nil
}

func (c *ContainerdSpawner) containerName(imageRef string) string {
	return sanitiseContainerRE.ReplaceAllString(imageRef, "_")
}

func (c *ContainerdSpawner) Prepare(ctx context.Context, imageRef string) error {
	log.Printf("Pulling image %s", imageRef)
	ctx = namespaces.WithNamespace(ctx, hostelNamespace)
	img, err := c.client.Pull(ctx, imageRef, containerd.WithPullUnpack)
	if err != nil {
		return errors.Wrap(err, "could not pull image with containerd")
	}

	id := c.containerName(imageRef)

	c.imageCache[id] = img

	return nil
}

func (c *ContainerdSpawner) Spawn(ctx context.Context, imageRef string, argv []string, in container.InteractiveInput) (<-chan container.ExitStatus, error) {
	ctx = namespaces.WithNamespace(ctx, hostelNamespace)
	key := c.containerName(imageRef)
	now := time.Now()
	id := fmt.Sprintf("%s-%d", key, now.UnixNano())

	img, ok := c.imageCache[key]
	if !ok {
		err := c.Prepare(ctx, imageRef)
		if err != nil {
			err = errors.Wrapf(err, "could not pull image %s", imageRef)
			log.Printf("error: %v", err)
			return nil, err
		}

		img = c.imageCache[key]
	}

	log.Printf("Creating container %s with %+v", id, argv)

	specOpts := []oci.SpecOpts{
		oci.WithImageConfigArgs(img, argv),
	}

	if size := in.Initial(); size != nil {
		specOpts = append(specOpts, oci.WithTTY, oci.WithTTYSize(int(size.Width), int(size.Height)))
	}

	ctr, err := c.client.NewContainer(
		ctx, id,
		containerd.WithNewSnapshotView(fmt.Sprintf("%s-rootfs", id), img),
		containerd.WithNewSpec(specOpts...))
	if err != nil {
		return nil, errors.Wrap(err, "unable to create container")
	}

	log.Printf("key: %s, map: %+v", key, c.imageCache)

	stdin := in.Stdin()
	stdout := in.Stdout()
	stderr := in.Stderr()

	log.Printf("Creating task in container %s", id)
	task, err := ctr.NewTask(ctx, cio.NewCreator(cio.WithStreams(stdin, stdout, stderr), cio.WithTerminal))
	if err != nil {
		cleanup(ctx, nil, ctr)
		return nil, errors.Wrap(err, "could not create task")
	}

	err = task.Start(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "could not start task")
	}

	log.Printf("Task %s started", task.ID())

	exitCh, err := task.Wait(ctx)
	if err != nil {
		cleanup(ctx, task, ctr)
		log.Printf("early exit: %v", err)
		return nil, errors.Wrap(err, "could not wait for task")
	}

	log.Printf("Task %s started", task.ID())

	ch := make(chan container.ExitStatus)
	go func() {
		defer stdout.Close()
		defer close(ch)
		defer cleanup(ctx, task, ctr)

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
			case <-ctx.Done():
				log.Printf("exiting early: %v", ctx.Err())
				return
			}
		}
	}()

	return ch, nil
}

func cleanup(ctx context.Context, task containerd.Task, c containerd.Container) {
	log.Printf("cleaning up task %+v", task)
	waitTime := 2 * time.Second

	newCtx := context.Background()
	newCtx, cancel := context.WithCancel(newCtx)
	defer cancel()

	if task != nil {
		waitCh, err := task.Wait(newCtx)
		if err != nil {
			log.Printf("cannot wait for task, bailing")
			return
		}

		log.Printf("cleaning up task %+v", task)
		for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL} {
			status, err := killAndWait(newCtx, syscall.SIGINT, task, waitCh, waitTime)
			if err != nil {
				log.Printf("Failed to kill task with %v: %v", sig, err)
			} else {
				log.Printf("Killed container with status %v", status)
				break
			}
		}

		if _, err := task.Delete(newCtx); err != nil {
			log.Printf("could not delete task: %v", err)
			return
		}
	}

	if c != nil {
		if err := c.Delete(newCtx); err != nil {
			log.Printf("could not clean up container %s: %v", c.ID(), err)
		}
	}
}

func killAndWait(ctx context.Context, sig syscall.Signal, task containerd.Task, waitCh <-chan containerd.ExitStatus, wait time.Duration) (containerd.ExitStatus, error) {
	select {
	case status := <-waitCh:
		return status, nil
	default:
	}

	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()

	log.Printf("Attempting to kill task %d with signal %v", task.Pid(), sig)
	err := task.Kill(ctx, sig)
	if err != nil {
		log.Printf("could not kill task: %v", err)
		return containerd.ExitStatus{}, errors.Wrap(err, "could not kill task: %v")
	}

	select {
	case st := <-waitCh:
		return st, nil
	case <-ctx.Done():
		return containerd.ExitStatus{}, ctx.Err()
	}
}
