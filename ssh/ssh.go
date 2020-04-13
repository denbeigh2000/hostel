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

type exitStatusMsg struct {
	Status uint32
}

func NewServer(authorizedKeysPath string, hostKey io.Reader, sessions config.UserSessionProvider, spawner container.Spawner) (*Server, error) {
	auth, err := newAuth(authorizedKeysPath)
	if err != nil {
		return nil, errors.Wrap(err, "not able to create auth object")
	}

	certChecker := ssh.CertChecker{
		UserKeyFallback: auth.Authenticate,
	}

	sshConf := &ssh.ServerConfig{
		BannerCallback: func(conn ssh.ConnMetadata) string {
			return fmt.Sprintf("User %s from %s\n", conn.User(), conn.RemoteAddr().String())
		},
		PublicKeyCallback: certChecker.Authenticate,
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
		auth,

		sessions,
		spawner,
	}, nil
}

type Server struct {
	sshConf *ssh.ServerConfig
	auth    *auth

	sessions config.UserSessionProvider
	spawner  container.Spawner
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
	conf := s.sessions.UserSession(username)

	for req := range reqs {
		defer channel.Close()

		log.Printf("handling request %s\n", req.Type)
		switch req.Type {
		case "exec", "shell":

			in := newInteractive(channel, reqs, size)
			outCh, err := s.handleShellExec(ctx, req, conf, in)
			if err != nil {
				req.Reply(false, []byte(err.Error()))
				return
			}

			req.Reply(true, nil)

			select {
			case <-ctx.Done():
				err := ctx.Err()
				switch err {
				case context.Canceled:
					fmt.Fprintf(channel, "exiting: server shutdown\n")
				case context.DeadlineExceeded:
					fmt.Fprintf(channel, "exiting: session open longer than %v\n", conf.MaxSessionDuration)
				}
			case exit := <-outCh:
				msg := ssh.Marshal(exitStatusMsg{Status: exit.Code})
				_, err := channel.SendRequest("exit-status", false, msg)
				if err != nil {
					log.Printf("could not send exit setatus back to %v@%v, %v", conn.User(), conn.RemoteAddr(), err)
				}
			}

			if err != nil {
				req.Reply(false, []byte(err.Error()))
			} else {
				req.Reply(true, nil)
			}

			return

		case "pty-req":
			err := handlePtyReq(req, size)
			if err != nil {
				req.Reply(false, []byte(err.Error()))
				continue
			}

			req.Reply(true, nil)

		default:
			req.Reply(false, nil)
		}
	}
}

func (s *Server) handleShellExec(ctx context.Context, req *ssh.Request, conf config.Session, in container.InteractiveInput) (<-chan container.ExitStatus, error) {
	var command []string
	if req.Type == "shell" {
		command = conf.DefaultArgv
	} else {
		var msg execMsg
		err := ssh.Unmarshal(req.Payload, &msg)
		if err != nil {
			return nil, errors.Wrap(err, "error unmarshaling command")
		}

		command = strings.Split(msg.Command, " ")
	}

	outCh, err := s.spawner.Spawn(ctx, conf.Image, command, in)
	if err != nil {
		return nil, errors.Wrap(err, "could not spawn process")
	}

	return outCh, nil
}

func handlePtyReq(req *ssh.Request, size *container.TermSize) error {
	if size != nil {
		return errors.New("pty already initialised")
	}

	var ptyReq ptyRequestMsg
	err := ssh.Unmarshal(req.Payload, &ptyReq)
	if err != nil {
		return errors.Errorf("failed to unmarshal payload: %v", err)
	}

	size = &container.TermSize{
		Width:  ptyReq.Columns,
		Height: ptyReq.Rows,
	}

	return nil
}
