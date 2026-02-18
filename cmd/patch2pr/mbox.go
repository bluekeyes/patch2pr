package main

import (
	"bytes"
	"io"
)

var (
	mboxHeader = []byte("From ")
)

type fileType int

const (
	fileUnknown fileType = iota
	filePlain
	fileMBox
)

type mboxMessageReader struct {
	r    io.Reader
	next bytes.Buffer

	headers int
	ftype   fileType

	isLineStart bool
	isEOF       bool
}

func (r *mboxMessageReader) Next() bool {
	if r.isEOF && r.bufferEmpty() {
		return false
	}

	r.headers = 0
	r.isLineStart = true
	return true
}

func (r *mboxMessageReader) Read(p []byte) (n int, err error) {
	// TODO(bkeyes): This is broken if it is only called with len(p) < len(mboxHeader).
	// This shouldn't be a problem in practice because go-gitdiff always wraps
	// the input reader in a bufio.Reader, which uses a large enough buffer.

	if len(p) == 0 {
		return 0, nil
	}
	if r.headers > 1 || (r.isEOF && r.bufferEmpty()) {
		return 0, io.EOF
	}

	if !r.bufferEmpty() {
		n, _ = r.next.Read(p)
		if r.bufferEmpty() {
			r.next.Reset()
		}
	}
	if n < len(p) {
		var nn int
		nn, err = r.r.Read(p[n:])
		if err == io.EOF {
			r.isEOF = true
			err = nil
		}
		n += nn
	}

	if r.ftype == fileUnknown || r.ftype == fileMBox {
		n = r.scanForHeader(p, n)
	}

	return n, err
}

func (r *mboxMessageReader) scanForHeader(p []byte, n int) int {
	for i := range n {
		if isSpace(p[i]) {
			r.isLineStart = p[i] == '\n'
			continue
		}

		if r.isLineStart {
			if matchLen := matchMBoxHeader(p[i:n]); matchLen > 0 {
				if matchLen == len(mboxHeader) {
					r.ftype = fileMBox
					r.headers++
				}
				if r.headers > 1 || matchLen < len(mboxHeader) {
					r.next.Write(p[i:n])
					return i
				}
			}
		}

		if r.ftype == fileUnknown {
			r.ftype = filePlain
			break
		}
	}
	return n
}

func (r *mboxMessageReader) bufferEmpty() bool {
	return r.next.Len() == 0
}

func matchMBoxHeader(b []byte) (n int) {
	for n < len(b) && n < len(mboxHeader) {
		if b[n] != mboxHeader[n] {
			return 0
		}
		n++
	}
	return n
}

func isSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\r', '\n':
		return true
	}
	return false
}
