package ssh

import (
	"fmt"
	"io"

	"github.com/denbeigh2000/hostel/container"

	"golang.org/x/crypto/ssh"
)

type sshInteractiveInput struct {
	ch      ssh.Channel
	reqs    <-chan *ssh.Request
	initial *container.TermSize

	sizeCh chan container.TermSize
}

func newInteractive(ch ssh.Channel, reqs <-chan *ssh.Request, initial *container.TermSize) *sshInteractiveInput {
	sizeCh := make(chan container.TermSize, 1)
	in := &sshInteractiveInput{ch, reqs, initial, sizeCh}
	go in.loop()
	return in
}

func (i *sshInteractiveInput) loop() {
	defer close(i.sizeCh)

	for req := range i.reqs {
		switch req.Type {
		case "window-change":
			var ptyReq ptyWindowChangeMsg
			err := ssh.Unmarshal(req.Payload, &ptyReq)
			if err != nil {
				req.Reply(false, []byte(fmt.Sprintf("failed to unmarshal payload: %v", err)))
				continue
			}

			i.sizeCh <- container.TermSize{
				Width:  ptyReq.Columns,
				Height: ptyReq.Rows,
			}

			req.Reply(true, nil)
		default:
			req.Reply(false, nil)
		}
	}
}

func (i *sshInteractiveInput) Stdin() io.Reader {
	return i.ch
}

func (i *sshInteractiveInput) Stdout() io.WriteCloser {
	return i.ch
}

func (i *sshInteractiveInput) Stderr() io.Writer {
	return i.ch.Stderr()
}

func (i *sshInteractiveInput) Initial() *container.TermSize {
	return i.initial
}

func (i *sshInteractiveInput) Updates() <-chan container.TermSize {
	return i.sizeCh
}
