package probe

import (
	"errors"
	"io"
	"os"
	"strings"
)

func readSmall(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, 1<<20))
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func splitLines(s string) []string { return strings.Split(s, "\n") }

func stripPrefix(s, prefix string) (string, bool) {
	if strings.HasPrefix(s, prefix) {
		return s[len(prefix):], true
	}
	return "", false
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// ErrNoConfig signals that the agent config file is missing — a
// well-formed exit condition rather than a hard failure, so the caller
// can print a friendlier "you need to enroll first" message.
var ErrNoConfig = errors.New("probe: agent not enrolled")
