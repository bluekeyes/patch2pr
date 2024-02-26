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
	if r.isEOF {
		return false
	}

	r.headers = 0
	r.isLineStart = true
	return true
}

func (r *mboxMessageReader) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.isEOF || r.headers > 1 {
		return 0, io.EOF
	}

	bufn, _ := r.next.Read(p)
	if bufn < len(p) {
		n, err = r.r.Read(p[bufn:])
		r.next.Reset()
	}
	n += bufn

	if r.ftype == fileUnknown || r.ftype == fileMBox {
		n = r.scanForHeader(p, n)
	}

	if err == io.EOF {
		r.isEOF = true
	}
	return n, err
}

func (r *mboxMessageReader) scanForHeader(p []byte, n int) int {
	for i := 0; i < n; i++ {
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
