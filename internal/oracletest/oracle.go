// Package oracletest talks to the GLib oracle (tools/oracle) in its --serve
// mode, for differential tests of the Go emulation against real GLib
// (design §11.4). The oracle command comes from the MYDUMPER_LINT_ORACLE
// environment variable, for example:
//
//	MYDUMPER_LINT_ORACLE="bin/oracle"
//	MYDUMPER_LINT_ORACLE="docker run -i --rm mydumper-lint-oracle:alma9"
//
// "--unsetenv" options are prepended so that the oracle runs with the same
// C locale as the emulation, and "--serve" (or "--plain --serve") is appended.
package oracletest

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// EnvVar names the environment variable holding the oracle command.
const EnvVar = "MYDUMPER_LINT_ORACLE"

// Verdict is the oracle's view of one file.
type Verdict struct {
	GLib     string
	Loadable bool
	Error    string // GLib's message when not loadable
	Groups   []Group
}

// Group is one group with its keys, as g_key_file_get_keys/get_value return them.
type Group struct {
	Name    string
	Entries []Entry
}

// Entry is one key and its value.
type Entry struct {
	Key, Value string
}

// Oracle is a running oracle process.
type Oracle struct {
	mu  sync.Mutex
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
}

// Start launches the oracle named by $MYDUMPER_LINT_ORACLE, loading files
// like mydumper v0.19.3-1 and later: after mydumper's pre-processor. It
// returns nil and no error when the variable is unset, so callers can skip.
func Start() (*Oracle, error) { return start(false) }

// StartPlain is Start for mydumper v0.19.1-x, which hands files to GLib
// unchanged.
func StartPlain() (*Oracle, error) { return start(true) }

func start(plain bool) (*Oracle, error) {
	spec := strings.Fields(os.Getenv(EnvVar))
	if len(spec) == 0 {
		return nil, nil
	}
	var args []string
	for _, v := range []string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG", "LC_CTYPE", "CHARSET"} {
		args = append(args, "--unsetenv", v)
	}
	if plain {
		args = append(args, "--plain")
	}
	args = append(args, "--serve")
	// The command comes from the developer's own environment variable.
	cmd := exec.Command(spec[0], append(spec[1:], args...)...) //nolint:gosec // G204/G702: test-only, trusted input
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting oracle %q: %w", spec, err)
	}
	return &Oracle{cmd: cmd, in: in, out: bufio.NewReader(out)}, nil
}

// Close stops the oracle.
func (o *Oracle) Close() error {
	if o == nil {
		return nil
	}
	return errors.Join(o.in.Close(), o.cmd.Wait())
}

// Load asks the oracle what GLib does with content (after mydumper's
// pre-processor, unless the oracle was started with StartPlain).
func (o *Oracle) Load(content []byte) (Verdict, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(content) > math.MaxUint32 {
		return Verdict{}, errors.New("content too large for the oracle protocol")
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(content))) //nolint:gosec // G115: bounded above
	if _, err := o.in.Write(append(hdr[:], content...)); err != nil {
		return Verdict{}, err
	}
	if _, err := io.ReadFull(o.out, hdr[:]); err != nil {
		return Verdict{}, fmt.Errorf("reading oracle response: %w", err)
	}
	resp := make([]byte, binary.BigEndian.Uint32(hdr[:]))
	if _, err := io.ReadFull(o.out, resp); err != nil {
		return Verdict{}, err
	}
	return decode(resp)
}

// wire is the oracle's JSON; every string encodes bytes as code points
// U+0000-U+00FF.
type wire struct {
	GLib     string  `json:"glib"`
	Loadable bool    `json:"loadable"`
	Error    *string `json:"error"`
	Groups   []struct {
		Name    string `json:"name"`
		Entries []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"entries"`
	} `json:"groups"`
}

func decode(b []byte) (Verdict, error) {
	var w wire
	if err := json.Unmarshal(b, &w); err != nil {
		return Verdict{}, fmt.Errorf("decoding %q: %w", b, err)
	}
	v := Verdict{GLib: w.GLib, Loadable: w.Loadable}
	var err error
	if w.Error != nil {
		if v.Error, err = bytesOf(*w.Error); err != nil {
			return v, err
		}
	}
	for _, g := range w.Groups {
		name, err := bytesOf(g.Name)
		if err != nil {
			return v, err
		}
		vg := Group{Name: name}
		for _, e := range g.Entries {
			k, err1 := bytesOf(e.Key)
			val, err2 := bytesOf(e.Value)
			if err := errors.Join(err1, err2); err != nil {
				return v, err
			}
			vg.Entries = append(vg.Entries, Entry{Key: k, Value: val})
		}
		v.Groups = append(v.Groups, vg)
	}
	return v, nil
}

// bytesOf maps each code point (≤ U+00FF) back to one byte.
func bytesOf(s string) (string, error) {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 0xff {
			return "", fmt.Errorf("code point %U out of the byte range", r)
		}
		b = append(b, byte(r)) //nolint:gosec // G115: r <= 0xff checked above
	}
	return string(b), nil
}
