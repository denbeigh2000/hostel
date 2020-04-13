package ssh

import (
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"
)

func newAuth(keyPath string) (*auth, error) {
	stat, err := os.Stat(keyPath)
	switch {
	case os.IsNotExist(err):
		return nil, errors.Errorf("authorized_keys dir does not exist at \"%s\"", keyPath)
	case err != nil:
		return nil, errors.Wrap(err, "not able to read authorized_keys directory")
	case !stat.IsDir():
		return nil, errors.New("authorized_keys path must be a directory")
	default:
		return &auth{authKeyPath: keyPath}, nil
	}
}

type auth struct {
	authKeyPath string
}

type key struct {
	Fingerprint string
	Comment     string
}

func (a auth) keyPathForUser(username string) string {
	return filepath.Join(a.authKeyPath, username)
}

func readAuthorizedKeys(path string) (map[string]key, error) {
	var (
		pubkey        ssh.PublicKey
		comment       string
		keys          = make(map[string]key)
		authdKeyBytes []byte
		err           error
	)

	log.Printf("loading authorized keys from %v", path)
	authdKeyBytes, err = ioutil.ReadFile(path)
	if err != nil {
		return nil, errors.Wrap(err, "could not read authorized_keys file")
	}

	for len(authdKeyBytes) > 0 {
		pubkey, comment, _, authdKeyBytes, err = ssh.ParseAuthorizedKey(authdKeyBytes)
		if err != nil {
			return nil, errors.Wrap(err, "could not parse authorized_keys entry")
		}
		fp := ssh.FingerprintSHA256(pubkey)
		keys[ssh.FingerprintSHA256(pubkey)] = key{
			Fingerprint: fp,
			Comment:     comment,
		}
	}

	return keys, nil
}

func (a *auth) Authenticate(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	username := c.User()
	keyPath := a.keyPathForUser(username)
	keys, err := readAuthorizedKeys(keyPath)
	if err != nil {
		log.Printf("Error reading keys for %v, %v", username, err)
		return nil, errors.Errorf("Unknown key for user %s", username)
	}

	fp := ssh.FingerprintSHA256(key)
	if key, ok := keys[fp]; ok {
		friendly := key.Comment
		if friendly == "" {
			friendly = fp
		}
		log.Printf("Authenticated %v using %v", username, friendly)

		return &ssh.Permissions{
			Extensions: map[string]string{
				"pubkey-fp": fp,
			},
		}, nil
	}

	return nil, errors.Errorf("Unknown key for use %s", username)
}
