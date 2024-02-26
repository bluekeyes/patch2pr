package main

import (
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestMBoxMessageReader_mboxFile(t *testing.T) {
	f, err := os.Open("testdata/test.mbox")
	if err != nil {
		t.Fatalf("error opening file: %v", err)
	}
	defer f.Close()

	mbr := mboxMessageReader{r: f}

	var msgs []string
	for i := 0; mbr.Next(); i++ {
		b, err := io.ReadAll(&mbr)
		if err != nil {
			t.Fatalf("unexpected error reading message %d: %v", i+1, err)
		}
		msgs = append(msgs, string(b))
	}

	assertMsgCount(t, msgs, 5)

	assertMsgContent(t, msgs, 0,
		`From 5255ca3071e33871ad7c23de1a3962f19b215f74 Mon Sep 17 00:00:00 2001
This is the first message

It has several
lines

`,
	)

	assertMsgContent(t, msgs, 1,
		`From 5a900b1a1f8b3e4244127bff85a5fd2d82ae2ced Mon Sep 17 00:00:00 2001
From: Test <test@example.com>
This is the second message

It has an mbox From header in the middle of a line and also has an email From: header

`,
	)

	assertMsgContent(t, msgs, 2,
		`From e32a4a21b36ccee78e91aa13933388a976bbd9da Mon Sep 17 00:00:00 2001

`,
	)

	assertMsgContent(t, msgs, 4,
		`From fdc67b11c12a7cdc7c9714b3b8ef95746198ee40 Mon Sep 17 00:00:00 2001

This is the last message in the file.
`,
	)
}

func TestMBoxMessageReader_regularFile(t *testing.T) {
	f, err := os.Open("testdata/test.txt")
	if err != nil {
		t.Fatalf("error opening file: %v", err)
	}
	defer f.Close()

	mbr := mboxMessageReader{r: f}

	var msgs []string
	for i := 0; mbr.Next(); i++ {
		b, err := io.ReadAll(&mbr)
		if err != nil {
			t.Fatalf("unexpected error reading message %d: %v", i+1, err)
		}
		msgs = append(msgs, string(b))
	}

	assertMsgCount(t, msgs, 1)

	expected, err := os.ReadFile("testdata/test.txt")
	if err != nil {
		t.Fatalf("error reading file: %v", err)
	}
	assertMsgContent(t, msgs, 0, string(expected))
}

func assertMsgCount(t *testing.T, msgs []string, count int) {
	if len(msgs) != count {
		msgStrs := make([]string, len(msgs))
		for _, m := range msgs {
			msgStrs = append(msgStrs, "  "+strconv.Quote(m))
		}

		t.Fatalf("incorrect number of messages: expected 1, got %d\nmessages: [%s,\n]", len(msgs), strings.Join(msgStrs, ",\n"))
	}
}

func assertMsgContent(t *testing.T, msgs []string, i int, expected string) {
	if msgs[i] != expected {
		t.Errorf("incorrect content for message %d:\nexpected: %q\n  actual: %q", i+1, expected, msgs[i])
	}
}
