package proxy

import (
	"net"
	"syscall"

	"github.com/pkg/errors"
)

var (
	// ErrUnknownHost denotes that the host could not be resolved.
	ErrUnknownHost = errors.New("unknown host")
	// ErrConnectionRefused denotes that the server refused the connection.
	ErrConnectionRefused = errors.New("connection refused")
	// ErrConnectionTimeout denotes that the server did not accept the connection before the timeout elapsed.
	ErrConnectionTimeout = errors.New("connection timeout")
)

// NormalizeNetworkError converts various network errors into easier to handle error variables.
func NormalizeNetworkError(err error) error {
	if netError, ok := err.(net.Error); ok && netError.Timeout() {
		return ErrConnectionTimeout
	}

	switch t := err.(type) {
	case *net.OpError:
		if t.Op == "dial" {
			return ErrUnknownHost
		} else if t.Op == "read" {
			return ErrConnectionRefused
		}
	case syscall.Errno:
		if t == syscall.ECONNREFUSED {
			return ErrConnectionRefused
		}
	}

	return err
}
