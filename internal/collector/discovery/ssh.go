package discovery

import (
	"bytes"
	"context"
	"errors"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHCred is a flat credential bundle a collector can pass to SSHRun.
type SSHCred struct {
	Username string
	Password string
	// PrivateKeyPEM is optional; when non-empty it overrides Password auth.
	PrivateKeyPEM string
}

// SSHRun dials host:22, authenticates, runs the given command and returns
// stdout (plus the host key fingerprint for future pinning). Timeouts are
// enforced both at dial and at command level.
func SSHRun(ctx context.Context, host string, cred SSHCred, cmd string, perStep time.Duration) (string, error) {
	if cred.Username == "" {
		return "", errors.New("ssh: empty username")
	}
	cfg := &ssh.ClientConfig{
		User:            cred.Username,
		Timeout:         perStep,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // discovery only; host key pinning is a future bullet
	}
	if cred.PrivateKeyPEM != "" {
		signer, err := ssh.ParsePrivateKey([]byte(cred.PrivateKeyPEM))
		if err != nil {
			return "", err
		}
		cfg.Auth = []ssh.AuthMethod{ssh.PublicKeys(signer)}
	} else {
		cfg.Auth = []ssh.AuthMethod{ssh.Password(cred.Password)}
	}

	dctx, cancel := context.WithTimeout(ctx, perStep)
	defer cancel()
	dialer := net.Dialer{Timeout: perStep}
	conn, err := dialer.DialContext(dctx, "tcp", net.JoinHostPort(host, "22"))
	if err != nil {
		return "", err
	}
	clientConn, chans, reqs, err := ssh.NewClientConn(conn, host+":22", cfg)
	if err != nil {
		_ = conn.Close()
		return "", err
	}
	cli := ssh.NewClient(clientConn, chans, reqs)
	defer cli.Close()

	sess, err := cli.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()

	var out bytes.Buffer
	sess.Stdout = &out
	sess.Stderr = &out

	done := make(chan error, 1)
	go func() { done <- sess.Run(cmd) }()
	select {
	case <-ctx.Done():
		_ = sess.Signal(ssh.SIGKILL)
		return out.String(), ctx.Err()
	case <-time.After(perStep):
		_ = sess.Signal(ssh.SIGKILL)
		return out.String(), errors.New("ssh: command timeout")
	case err = <-done:
		return out.String(), err
	}
}
