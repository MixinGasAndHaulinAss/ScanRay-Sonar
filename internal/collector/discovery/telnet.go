package discovery

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"time"
)

// TelnetCred is the pair of credentials used by TelnetRun. Many vendor CLIs
// require an "enable" or privileged-EXEC password after login.
type TelnetCred struct {
	Username     string
	Password     string
	EnablePass   string // optional, sent if the prompt ends with `>` after login
	LoginPrompt  string // default: "ogin:"
	PassPrompt   string // default: "assword:"
	EnablePrompt string // default: "#"
}

// TelnetRun dials host:23, performs a minimal IAC negotiation (rejects all
// options it doesn't speak), logs in, runs the given command, and returns
// stdout. This is intentionally simple — many devices accept a no-op IAC
// negotiation. Use SSH where possible.
func TelnetRun(ctx context.Context, host string, cred TelnetCred, cmd string, perStep time.Duration) (string, error) {
	dialer := net.Dialer{Timeout: perStep}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, "23"))
	if err != nil {
		return "", err
	}
	defer conn.Close()
	if d, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(d)
	} else {
		_ = conn.SetDeadline(time.Now().Add(perStep))
	}
	loginPrompt := defaultStr(cred.LoginPrompt, "ogin:")
	passPrompt := defaultStr(cred.PassPrompt, "assword:")
	enablePrompt := defaultStr(cred.EnablePrompt, "#")

	if err := telnetExpect(conn, loginPrompt, perStep); err != nil {
		return "", err
	}
	if _, err := conn.Write([]byte(cred.Username + "\r\n")); err != nil {
		return "", err
	}
	if err := telnetExpect(conn, passPrompt, perStep); err != nil {
		return "", err
	}
	if _, err := conn.Write([]byte(cred.Password + "\r\n")); err != nil {
		return "", err
	}
	// Wait for a generic prompt: ">" (user EXEC), "#" (privileged), or "$" (shell).
	out, prompt, err := telnetReadUntilAny(conn, []string{">", "#", "$"}, perStep)
	if err != nil {
		return out, err
	}
	if prompt == ">" && cred.EnablePass != "" {
		if _, err := conn.Write([]byte("enable\r\n")); err != nil {
			return out, err
		}
		if err := telnetExpect(conn, passPrompt, perStep); err != nil {
			return out, err
		}
		if _, err := conn.Write([]byte(cred.EnablePass + "\r\n")); err != nil {
			return out, err
		}
		if err := telnetExpect(conn, enablePrompt, perStep); err != nil {
			return out, err
		}
	}
	if _, err := conn.Write([]byte(cmd + "\r\n")); err != nil {
		return out, err
	}
	body, _, err := telnetReadUntilAny(conn, []string{enablePrompt, "$ "}, perStep)
	if err != nil && !errors.Is(err, io.EOF) {
		return body, err
	}
	return body, nil
}

func defaultStr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func telnetExpect(conn net.Conn, marker string, perStep time.Duration) error {
	_, _, err := telnetReadUntilAny(conn, []string{marker}, perStep)
	return err
}

// telnetReadUntilAny consumes bytes until any of the markers appears, while
// silently rejecting any IAC command sub-negotiations the peer sends.
func telnetReadUntilAny(conn net.Conn, markers []string, perStep time.Duration) (string, string, error) {
	deadline := time.Now().Add(perStep)
	_ = conn.SetReadDeadline(deadline)
	var buf bytes.Buffer
	tmp := make([]byte, 256)
	for {
		n, err := conn.Read(tmp)
		if n > 0 {
			cleaned := stripIAC(tmp[:n], conn)
			buf.Write(cleaned)
			s := buf.String()
			for _, m := range markers {
				if strings.Contains(s, m) {
					return s, m, nil
				}
			}
		}
		if err != nil {
			return buf.String(), "", err
		}
		if time.Now().After(deadline) {
			return buf.String(), "", errors.New("telnet: read deadline exceeded")
		}
	}
}

// stripIAC drops Telnet IAC option bytes and replies WONT/DONT to keep the
// peer from waiting forever. Anything outside an IAC sequence is preserved.
func stripIAC(in []byte, conn net.Conn) []byte {
	const (
		IAC  = 255
		DONT = 254
		DO   = 253
		WONT = 252
		WILL = 251
		SB   = 250
		SE   = 240
	)
	out := make([]byte, 0, len(in))
	for i := 0; i < len(in); i++ {
		b := in[i]
		if b != IAC {
			out = append(out, b)
			continue
		}
		if i+1 >= len(in) {
			break
		}
		cmd := in[i+1]
		switch cmd {
		case DO, DONT, WILL, WONT:
			if i+2 >= len(in) {
				return out
			}
			opt := in[i+2]
			// Refuse everything: WILL -> DONT, DO -> WONT.
			var reply byte
			if cmd == DO {
				reply = WONT
			} else if cmd == WILL {
				reply = DONT
			}
			if reply != 0 {
				_, _ = conn.Write([]byte{IAC, reply, opt})
			}
			i += 2
		case SB:
			// Skip until IAC SE.
			j := i + 2
			for j+1 < len(in) {
				if in[j] == IAC && in[j+1] == SE {
					break
				}
				j++
			}
			i = j + 1
		default:
			i++ // unknown 2-byte command; skip
		}
	}
	return out
}
