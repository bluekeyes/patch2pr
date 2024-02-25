package main

import (
	"bytes"
	"io"
)

var (
	mboxHeader = []byte("From ")
)

type mboxMessageReader struct {
	r    io.Reader
	next bytes.Buffer

	headers int
	isLine  bool
	isEOF   bool
}

func (r *mboxMessageReader) Next() bool {
	if r.isEOF {
		return false
	}

	r.headers = 0
	r.isLine = true
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

	for i := 0; i < n; i++ {
		if r.isLine {
			if matchLen := matchMBoxHeader(p[i:n]); matchLen > 0 {
				if matchLen == len(mboxHeader) {
					r.headers++
				}
				if r.headers > 1 || matchLen < len(mboxHeader) {
					r.next.Write(p[i:n])
					n = i
					break
				}
			}
		}
		r.isLine = p[i] == '\n'
	}

	if err == io.EOF {
		r.isEOF = true
	}
	return n, err
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
