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
	case *net.DNSError:
		return ErrUnknownHost
	case *net.OpError:
		switch t.Op {
		case "dial":
			return ErrConnectionRefused
		case "read":
			return ErrConnectionRefused
		}
	case syscall.Errno:
		switch t {
		case syscall.ECONNREFUSED, syscall.EHOSTUNREACH, syscall.EHOSTDOWN:
			return ErrConnectionRefused
		}
	}

	return err
}

// HandleNetworkError responses with the appropriate error depending on the error type.
func HandleNetworkError(stream *Stream, err error) {
	err = NormalizeNetworkError(errors.Cause(err))

	switch err {
	case ErrNotFound:
		RespondWithError(stream, err, 404)
	case ErrServiceUnavailable, ErrUnknownHost, ErrConnectionRefused:
		RespondWithError(stream, err, 503)
	case ErrConnectionTimeout:
		RespondWithError(stream, err, 504)
	default:
		RespondWithError(stream, ErrInternalServerError, 500)
	}
}
