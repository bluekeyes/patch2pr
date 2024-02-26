package main

import (
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestMBoxMessageReader(t *testing.T) {
	plainMessage, err := os.ReadFile("testdata/test.txt")
	if err != nil {
		t.Fatalf("error reading file: %v", err)
	}

	tests := map[string]struct {
		File            string
		Count           int
		ExpectedContent map[int]string
	}{
		"mbox": {
			File:  "testdata/test.mbox",
			Count: 5,
			ExpectedContent: map[int]string{
				0: `From 5255ca3071e33871ad7c23de1a3962f19b215f74 Mon Sep 17 00:00:00 2001
This is the first message

It has several
lines

`,
				1: `From 5a900b1a1f8b3e4244127bff85a5fd2d82ae2ced Mon Sep 17 00:00:00 2001
From: Test <test@example.com>
This is the second message

It has an mbox From header in the middle of a line and also has an email From: header

`,
				2: `From e32a4a21b36ccee78e91aa13933388a976bbd9da Mon Sep 17 00:00:00 2001

`,
				4: `From fdc67b11c12a7cdc7c9714b3b8ef95746198ee40 Mon Sep 17 00:00:00 2001

This is the last message in the file.
`,
			},
		},
		"short": {
			File:  "testdata/short.mbox",
			Count: 3,
		},
		"plain": {
			File:            "testdata/test.txt",
			Count:           1,
			ExpectedContent: map[int]string{0: string(plainMessage)},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			f, err := os.Open(test.File)
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

			assertMsgCount(t, msgs, test.Count)
			for key, msg := range test.ExpectedContent {
				assertMsgContent(t, msgs, key, msg)
			}
		})
	}
}

func assertMsgCount(t *testing.T, msgs []string, count int) {
	if len(msgs) != count {
		msgStrs := make([]string, len(msgs))
		for i, m := range msgs {
			msgStrs[i] = "  " + strconv.Quote(m)
		}

		t.Fatalf("incorrect number of messages: expected %d, got %d\nmessages: [\n%s,\n]", count, len(msgs), strings.Join(msgStrs, ",\n"))
	}
}

func assertMsgContent(t *testing.T, msgs []string, i int, expected string) {
	if msgs[i] != expected {
		t.Errorf("incorrect content for message %d:\nexpected: %q\n  actual: %q", i+1, expected, msgs[i])
	}
}
