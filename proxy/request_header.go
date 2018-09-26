// Copyright 2018 The Prizem Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package proxy

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/net/http2/hpack"
)

var (
	strHTTP11 = []byte("HTTP/1.1")
	strGet    = []byte("GET")
	strHead   = []byte("HEAD")
	strPost   = []byte("POST")
	strPut    = []byte("PUT")
	strDelete = []byte("DELETE")

	strConnection         = []byte("Connection")
	strClose              = []byte("close")
	strIdentity           = []byte("identity")
	strChunked            = []byte("chunked")
	strTransferEncoding   = []byte("Transfer-Encoding")
	strKeepAlive          = []byte("keep-alive")
	strKeepAliveCamelCase = []byte("Keep-Alive")
	strCRLF               = []byte("\r\n")

	errNeedMore = errors.New("need more data: cannot find trailing lf")
)

type RequestHeader struct {
	//noCopy noCopy

	//disableNormalizing bool
	noHTTP11        bool
	connectionClose bool
	isGet           bool

	contentLength      int
	contentLengthBytes []byte

	method      []byte
	requestURI  []byte
	protocol    []byte
	host        []byte
	contentType []byte
	userAgent   []byte

	//h     []argsKV
	//bufKV argsKV

	//cookies []argsKV
	headers Headers

	rawHeaders []byte

	chunked   bool
	keepAlive bool
}

// Read reads request header from r.
//
// io.EOF is returned if r is closed before reading the first header byte.
func (h *RequestHeader) Read(r *bufio.Reader) error {
	n := 1
	for {
		err := h.tryRead(r, n)
		if err == nil {
			return nil
		}
		if err != errNeedMore {
			h.resetSkipNormalize()
			return err
		}
		n = r.Buffered() + 1
	}
}

func (h *RequestHeader) resetSkipNormalize() {
	h.noHTTP11 = false
	h.connectionClose = false
	h.isGet = false

	h.contentLength = 0
	h.contentLengthBytes = h.contentLengthBytes[:0]

	h.method = h.method[:0]
	h.protocol = h.protocol[:0]
	h.requestURI = h.requestURI[:0]
	h.host = h.host[:0]
	h.contentType = h.contentType[:0]
	h.userAgent = h.userAgent[:0]

	h.headers = h.headers[:0]
	//h.cookies = h.cookies[:0]
	//h.cookiesCollected = false

	h.rawHeaders = h.rawHeaders[:0]
}

var errSmallBuffer = errors.New("small read buffer. Increase ReadBufferSize")

func (h *RequestHeader) tryRead(r *bufio.Reader, n int) error {
	h.resetSkipNormalize()
	b, err := r.Peek(n)
	if len(b) == 0 {
		// treat all errors on the first byte read as EOF
		if n == 1 || err == io.EOF {
			return io.EOF
		}

		// This is for go 1.6 bug. See https://github.com/golang/go/issues/14121 .
		if err == bufio.ErrBufferFull {
			return &ErrSmallBuffer{
				error: fmt.Errorf("error when reading request headers: %s", errSmallBuffer),
			}
		}

		return fmt.Errorf("error when reading request headers: %s", err)
	}
	b = mustPeekBuffered(r)
	headersLen, errParse := h.parse(b)
	if errParse != nil {
		return headerError("request", err, errParse, b)
	}
	mustDiscard(r, headersLen)
	return nil
}

// ErrSmallBuffer is returned when the provided buffer size is too small
// for reading request and/or response headers.
//
// ReadBufferSize value from Server or clients should reduce the number
// of such errors.
type ErrSmallBuffer struct {
	error
}

func headerError(typ string, err, errParse error, b []byte) error {
	if errParse != errNeedMore {
		return headerErrorMsg(typ, errParse, b)
	}
	if err == nil {
		return errNeedMore
	}

	// Buggy servers may leave trailing CRLFs after http body.
	// Treat this case as EOF.
	if isOnlyCRLF(b) {
		return io.EOF
	}

	if err != bufio.ErrBufferFull {
		return headerErrorMsg(typ, err, b)
	}
	return &ErrSmallBuffer{
		error: headerErrorMsg(typ, errSmallBuffer, b),
	}
}

func bufferSnippet(b []byte) string {
	n := len(b)
	start := 200
	end := n - start
	if start >= end {
		start = n
		end = n
	}
	bStart, bEnd := b[:start], b[end:]
	if len(bEnd) == 0 {
		return fmt.Sprintf("%q", b)
	}
	return fmt.Sprintf("%q...%q", bStart, bEnd)
}

func isOnlyCRLF(b []byte) bool {
	for _, ch := range b {
		if ch != '\r' && ch != '\n' {
			return false
		}
	}
	return true
}

func headerErrorMsg(typ string, err error, b []byte) error {
	return fmt.Errorf("error when reading %s headers: %s. Buffer size=%d, contents: %s", typ, err, len(b), bufferSnippet(b))
}

func mustPeekBuffered(r *bufio.Reader) []byte {
	buf, err := r.Peek(r.Buffered())
	if len(buf) == 0 || err != nil {
		panic(fmt.Sprintf("bufio.Reader.Peek() returned unexpected data (%q, %v)", buf, err))
	}
	return buf
}

func mustDiscard(r *bufio.Reader, n int) {
	if _, err := r.Discard(n); err != nil {
		panic(fmt.Sprintf("bufio.Reader.Discard(%d) failed: %s", n, err))
	}
}

func (h *RequestHeader) parse(buf []byte) (int, error) {
	m, err := h.parseFirstLine(buf)
	if err != nil {
		return 0, err
	}

	var n int
	n, err = h.parseHeaders(buf[m:])
	if err != nil {
		return 0, err
	}
	return m + n, nil
}

func (h *RequestHeader) parseFirstLine(buf []byte) (int, error) {
	bNext := buf
	var b []byte
	var err error
	for len(b) == 0 {
		if b, bNext, err = nextLine(bNext); err != nil {
			return 0, err
		}
	}

	// parse method
	n := bytes.IndexByte(b, ' ')
	if n <= 0 {
		return 0, fmt.Errorf("cannot find http request method in %q", buf)
	}
	h.method = append(h.method[:0], b[:n]...)
	b = b[n+1:]

	// parse requestURI
	n = bytes.LastIndexByte(b, ' ')
	if n < 0 {
		h.noHTTP11 = true
		n = len(b)
	} else if n == 0 {
		return 0, fmt.Errorf("requestURI cannot be empty in %q", buf)
	} else {
		h.protocol = b[n+1:]
		if !bytes.Equal(h.protocol, strHTTP11) {
			h.noHTTP11 = true
		}
	}
	h.requestURI = append(h.requestURI[:0], b[:n]...)

	return len(buf) - len(bNext), nil
}

func (h *RequestHeader) parseHeaders(buf []byte) (int, error) {
	h.contentLength = -2

	var s headerScanner
	s.b = buf
	//s.disableNormalizing = h.disableNormalizing
	var err error
	for s.next() {
		headerName := strings.ToLower(string(s.key))
		switch headerName {
		case "host":
			h.host = append(h.host[:0], s.value...)
		case "user-agent":
			h.userAgent = append(h.userAgent[:0], s.value...)
		case "content-type":
			h.contentType = append(h.contentType[:0], s.value...)
			h.headers = append(h.headers, hpack.HeaderField{
				Name:  "content-type",
				Value: string(s.value),
			})
		case "content-length":
			if h.contentLength != -1 {
				println(string(s.value))
				if h.contentLength, err = parseContentLength(s.value); err != nil {
					h.contentLength = -2
				} else {
					h.contentLengthBytes = append(h.contentLengthBytes[:0], s.value...)
				}
			}
		case "transfer-encoding":
			if !bytes.Equal(s.value, strIdentity) {
				h.contentLength = -1
				h.chunked = true
				//h.h = setArgBytes(h.h, strTransferEncoding, strChunked)
			}
		case "connection":
			if bytes.Equal(s.value, strClose) {
				h.connectionClose = true
			} else {
				if bytes.Equal(s.value, strKeepAlive) || bytes.Equal(s.value, strKeepAliveCamelCase) {
					h.keepAlive = true
				}
				h.connectionClose = false
				//h.h = appendArgBytes(h.h, s.key, s.value)
			}
		default:
			//h.h = appendArgBytes(h.h, s.key, s.value)
			h.headers = append(h.headers, hpack.HeaderField{
				Name:  headerName,
				Value: string(s.value),
			})
		}
	}
	if s.err != nil {
		h.connectionClose = true
		return 0, s.err
	}

	if h.contentLength < 0 {
		h.contentLengthBytes = h.contentLengthBytes[:0]
	}
	if h.noBody() {
		h.contentLength = 0
		h.contentLengthBytes = h.contentLengthBytes[:0]
	}
	if h.noHTTP11 && !h.connectionClose {
		// close connection for non-http/1.1 request unless 'Connection: keep-alive' is set.
		h.connectionClose = !h.keepAlive
	}

	return len(buf) - len(s.b), nil
}

func (h *RequestHeader) noBody() bool {
	return h.IsGet() || h.IsHead()
}

// IsGet returns true if request method is GET.
func (h *RequestHeader) IsGet() bool {
	// Optimize fast path for GET requests.
	if !h.isGet {
		h.isGet = bytes.Equal(h.Method(), strGet)
	}
	return h.isGet
}

// IsPost returns true if request methos is POST.
func (h *RequestHeader) IsPost() bool {
	return bytes.Equal(h.Method(), strPost)
}

// IsPut returns true if request method is PUT.
func (h *RequestHeader) IsPut() bool {
	return bytes.Equal(h.Method(), strPut)
}

// IsHead returns true if request method is HEAD.
func (h *RequestHeader) IsHead() bool {
	// Fast path
	if h.isGet {
		return false
	}
	return bytes.Equal(h.Method(), strHead)
}

// IsDelete returns true if request method is DELETE.
func (h *RequestHeader) IsDelete() bool {
	return bytes.Equal(h.Method(), strDelete)
}

// Method returns HTTP request method.
func (h *RequestHeader) Method() []byte {
	if len(h.method) == 0 {
		return strGet
	}
	return h.method
}

// IsHTTP11 returns true if the request is HTTP/1.1.
func (h *RequestHeader) IsHTTP11() bool {
	return !h.noHTTP11
}

func nextLine(b []byte) ([]byte, []byte, error) {
	nNext := bytes.IndexByte(b, '\n')
	if nNext < 0 {
		return nil, nil, errNeedMore
	}
	n := nNext
	if n > 0 && b[n-1] == '\r' {
		n--
	}
	return b[:n], b[nNext+1:], nil
}

type headerScanner struct {
	b     []byte
	key   []byte
	value []byte
	err   error

	disableNormalizing bool
}

func (s *headerScanner) next() bool {
	bLen := len(s.b)
	if bLen >= 2 && s.b[0] == '\r' && s.b[1] == '\n' {
		s.b = s.b[2:]
		return false
	}
	if bLen >= 1 && s.b[0] == '\n' {
		s.b = s.b[1:]
		return false
	}
	n := bytes.IndexByte(s.b, ':')
	if n < 0 {
		s.err = errNeedMore
		return false
	}
	s.key = s.b[:n]
	//normalizeHeaderKey(s.key, s.disableNormalizing)
	n++
	for len(s.b) > n && s.b[n] == ' ' {
		n++
	}
	s.b = s.b[n:]
	n = bytes.IndexByte(s.b, '\n')
	if n < 0 {
		s.err = errNeedMore
		return false
	}
	s.value = s.b[:n]
	s.b = s.b[n+1:]

	if n > 0 && s.value[n-1] == '\r' {
		n--
	}
	for n > 0 && s.value[n-1] == ' ' {
		n--
	}
	s.value = s.value[:n]
	return true
}

func parseContentLength(b []byte) (int, error) {
	v, n, err := parseUintBuf(b)
	if err != nil {
		return -1, err
	}
	if n != len(b) {
		return -1, fmt.Errorf("non-numeric chars at the end of Content-Length")
	}
	return v, nil
}

var (
	errEmptyInt               = errors.New("empty integer")
	errUnexpectedFirstChar    = errors.New("unexpected first char found. Expecting 0-9")
	errUnexpectedTrailingChar = errors.New("unexpected traling char found. Expecting 0-9")
	errTooLongInt             = errors.New("too long int")
)

const (
	maxIntChars    = 9
	maxHexIntChars = 7
)

func parseUintBuf(b []byte) (int, int, error) {
	n := len(b)
	if n == 0 {
		return -1, 0, errEmptyInt
	}
	v := 0
	for i := 0; i < n; i++ {
		c := b[i]
		k := c - '0'
		if k > 9 {
			if i == 0 {
				return -1, i, errUnexpectedFirstChar
			}
			return v, i, nil
		}
		if i >= maxIntChars {
			return -1, i, errTooLongInt
		}
		v = 10*v + int(k)
	}
	return v, n, nil
}

// ErrBodyTooLarge is returned if either request or response body exceeds
// the given limit.
var ErrBodyTooLarge = errors.New("body size exceeds the given limit")

func readBody(r *bufio.Reader, contentLength int, maxBodySize int, dst []byte) ([]byte, error) {
	dst = dst[:0]
	if contentLength >= 0 {
		if maxBodySize > 0 && contentLength > maxBodySize {
			return dst, ErrBodyTooLarge
		}
		return appendBodyFixedSize(r, dst, contentLength)
	}
	if contentLength == -1 {
		return readBodyChunked(r, maxBodySize, dst)
	}
	return readBodyIdentity(r, maxBodySize, dst)
}

func readBodyIdentity(r *bufio.Reader, maxBodySize int, dst []byte) ([]byte, error) {
	dst = dst[:cap(dst)]
	if len(dst) == 0 {
		dst = make([]byte, 1024)
	}
	offset := 0
	for {
		nn, err := r.Read(dst[offset:])
		if nn <= 0 {
			if err != nil {
				if err == io.EOF {
					return dst[:offset], nil
				}
				return dst[:offset], err
			}
			panic(fmt.Sprintf("BUG: bufio.Read() returned (%d, nil)", nn))
		}
		offset += nn
		if maxBodySize > 0 && offset > maxBodySize {
			return dst[:offset], ErrBodyTooLarge
		}
		if len(dst) == offset {
			n := round2(2 * offset)
			if maxBodySize > 0 && n > maxBodySize {
				n = maxBodySize + 1
			}
			b := make([]byte, n)
			copy(b, dst)
			dst = b
		}
	}
}

func appendBodyFixedSize(r *bufio.Reader, dst []byte, n int) ([]byte, error) {
	if n == 0 {
		return dst, nil
	}

	offset := len(dst)
	dstLen := offset + n
	if cap(dst) < dstLen {
		b := make([]byte, round2(dstLen))
		copy(b, dst)
		dst = b
	}
	dst = dst[:dstLen]

	for {
		nn, err := r.Read(dst[offset:])
		if nn <= 0 {
			if err != nil {
				if err == io.EOF {
					err = io.ErrUnexpectedEOF
				}
				return dst[:offset], err
			}
			panic(fmt.Sprintf("BUG: bufio.Read() returned (%d, nil)", nn))
		}
		offset += nn
		if offset == dstLen {
			return dst, nil
		}
	}
}

func readBodyChunked(r *bufio.Reader, maxBodySize int, dst []byte) ([]byte, error) {
	if len(dst) > 0 {
		panic("BUG: expected zero-length buffer")
	}

	strCRLFLen := len(strCRLF)
	for {
		chunkSize, err := parseChunkSize(r)
		if err != nil {
			return dst, err
		}
		if maxBodySize > 0 && len(dst)+chunkSize > maxBodySize {
			return dst, ErrBodyTooLarge
		}
		dst, err = appendBodyFixedSize(r, dst, chunkSize+strCRLFLen)
		if err != nil {
			return dst, err
		}
		if !bytes.Equal(dst[len(dst)-strCRLFLen:], strCRLF) {
			return dst, fmt.Errorf("cannot find crlf at the end of chunk")
		}
		dst = dst[:len(dst)-strCRLFLen]
		if chunkSize == 0 {
			return dst, nil
		}
	}
}

func parseChunkSize(r *bufio.Reader) (int, error) {
	n, err := readHexInt(r)
	if err != nil {
		return -1, err
	}
	c, err := r.ReadByte()
	if err != nil {
		return -1, fmt.Errorf("cannot read '\r' char at the end of chunk size: %s", err)
	}
	if c != '\r' {
		return -1, fmt.Errorf("unexpected char %q at the end of chunk size. Expected %q", c, '\r')
	}
	c, err = r.ReadByte()
	if err != nil {
		return -1, fmt.Errorf("cannot read '\n' char at the end of chunk size: %s", err)
	}
	if c != '\n' {
		return -1, fmt.Errorf("unexpected char %q at the end of chunk size. Expected %q", c, '\n')
	}
	return n, nil
}

func round2(n int) int {
	if n <= 0 {
		return 0
	}
	n--
	x := uint(0)
	for n > 0 {
		n >>= 1
		x++
	}
	return 1 << x
}

var (
	errEmptyHexNum    = errors.New("empty hex number")
	errTooLargeHexNum = errors.New("too large hex number")
)

var hex2intTable = func() []byte {
	b := make([]byte, 256)
	for i := 0; i < 256; i++ {
		c := byte(16)
		if i >= '0' && i <= '9' {
			c = byte(i) - '0'
		} else if i >= 'a' && i <= 'f' {
			c = byte(i) - 'a' + 10
		} else if i >= 'A' && i <= 'F' {
			c = byte(i) - 'A' + 10
		}
		b[i] = c
	}
	return b
}()

func readHexInt(r *bufio.Reader) (int, error) {
	n := 0
	i := 0
	var k int
	for {
		c, err := r.ReadByte()
		if err != nil {
			if err == io.EOF && i > 0 {
				return n, nil
			}
			return -1, err
		}
		k = int(hex2intTable[c])
		if k == 16 {
			if i == 0 {
				return -1, errEmptyHexNum
			}
			r.UnreadByte()
			return n, nil
		}
		if i >= maxHexIntChars {
			return -1, errTooLargeHexNum
		}
		n = (n << 4) | k
		i++
	}
}
