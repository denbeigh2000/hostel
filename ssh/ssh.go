package ssh

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"strings"

	"github.com/denbeigh2000/hostel/config"
	"github.com/denbeigh2000/hostel/container"

	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"
)

// RFC 4254 Section 6.2.
type ptyRequestMsg struct {
	Term     string
	Columns  uint32
	Rows     uint32
	Width    uint32
	Height   uint32
	Modelist string
}

// RFC 4254 Section 6.5.
type execMsg struct {
	Command string
}

// RFC 4254 Section 6.7.
type ptyWindowChangeMsg struct {
	Columns uint32
	Rows    uint32
	Width   uint32
	Height  uint32
}

func NewServer(hostKey io.Reader, conf config.Global, spawner container.Spawner) (*Server, error) {
	sshConf := &ssh.ServerConfig{
		BannerCallback: func(conn ssh.ConnMetadata) string {
			return fmt.Sprintf("User %s from %s\n", conn.User(), conn.RemoteAddr().String())
		},
	}

	keyBytes, err := ioutil.ReadAll(hostKey)
	if err != nil {
		return nil, errors.Wrap(err, "couldn't read host key")
	}

	key, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, errors.Wrap(err, "couldn't parse host key")
	}

	sshConf.AddHostKey(key)

	return &Server{
		sshConf,
		conf,
		spawner,
	}, nil
}

type Server struct {
	sshConf *ssh.ServerConfig

	conf    config.Global
	spawner container.Spawner
}

func (s *Server) listen(l *net.TCPListener) error {
	log.Printf("Serving on %v", l.Addr())

	for {
		conn, err := l.AcceptTCP()
		if err != nil {
			return err
		}

		go s.handleConn(conn, s.sshConf)
	}
}

func (s *Server) handleConn(tConn *net.TCPConn, config *ssh.ServerConfig) {
	conn, chans, reqs, err := ssh.NewServerConn(tConn, config)
	if err != nil {
		log.Printf("Failed to create SSH connection: %s", err)
		return
	}

	log.Printf("Serving SSH connection from %v", conn.RemoteAddr())

	go ssh.DiscardRequests(reqs)
	// Service the incoming Channel channel.
	for newChannel := range chans {
		switch newChannel.ChannelType() {
		case "session":
			channel, requests, err := newChannel.Accept()
			if err != nil {
				log.Printf("Could not accept channel: %v", err)
			}

			// TODO
			go ssh.DiscardRequests(requests)
			channel.Close()

		default:
			newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}
	}
}

func (s *Server) handleSession(
	ctx context.Context,
	conn ssh.ConnMetadata,
	channel ssh.Channel,
	reqs <-chan *ssh.Request,
) {
	var size *container.TermSize
	username := conn.User()
	conf := s.conf.Defaults
	user, ok := s.conf.UserOverrides[username]
	if ok {
		if user.DefaultArgv != nil {
			conf.DefaultArgv = user.DefaultArgv
		}
		if user.Image != "" {
			conf.Image = user.Image
		}
		if user.MaxSessionDuration > 0 {
			conf.MaxSessionDuration = user.MaxSessionDuration
		}
	}

	for req := range reqs {
		defer channel.Close()

		log.Printf("handling request %s\n", req.Type)
		switch req.Type {
		case "exec", "shell":

			var command []string
			if req.Type == "shell" {
				command = conf.DefaultArgv
			} else {
				var msg execMsg
				err := ssh.Unmarshal(req.Payload, &msg)
				if err != nil {
					req.Reply(false, []byte(fmt.Sprintf("error unmarshaling command: %v", err)))
					return
				}

				command = strings.Split(msg.Command, " ")
			}

			in := newInteractive(channel, reqs, size)

			req.Reply(true, nil)
			err := s.spawner.Spawn(ctx, "", "", command, in)
			if err != nil {
				log.Printf("error spawning process: %v", err)
			}
			return

		case "pty-req":
			if size != nil {
				req.Reply(false, []byte("already initialised"))
				continue
			}

			var ptyReq ptyRequestMsg
			err := ssh.Unmarshal(req.Payload, &ptyReq)
			if err != nil {
				req.Reply(false, []byte(fmt.Sprintf("failed to unmarshal payload: %v", err)))
				continue
			}

			size = &container.TermSize{
				Width:  ptyReq.Columns,
				Height: ptyReq.Rows,
			}
			req.Reply(true, nil)

		default:
			req.Reply(false, nil)
		}
	}
}
