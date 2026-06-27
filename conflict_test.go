package patch2pr

import (
	"errors"
	"testing"
)

func TestConflictError(t *testing.T) {
	for i, tc := range []struct {
		Conflict Conflict
		Error    string
	}{
		{
			Conflict{},
			"conflict",
		},
		{
			Conflict{File: "path/to/file.txt"},
			"path/to/file.txt: conflict",
		},
		{
			Conflict{File: "path/to/file.txt", Type: ConflictNewFileExists},
			"path/to/file.txt: conflict: new file already exists",
		},
		{
			Conflict{File: "path/to/file.txt", Type: ConflictDeletedFileMissing},
			"path/to/file.txt: conflict: deleted file does not exist",
		},
		{
			Conflict{File: "path/to/file.txt", Type: ConflictModifiedFileMissing},
			"path/to/file.txt: conflict: modified file does not exist",
		},
		{
			Conflict{File: "path/to/file.txt", Type: ConflictContent},
			"path/to/file.txt: conflict: content",
		},
		{
			Conflict{File: "path/to/file.txt", Line: 23, Type: ConflictContent},
			"path/to/file.txt:23: conflict: content",
		},
	} {
		want := tc.Error
		if got := tc.Conflict.Error(); got != want {
			t.Errorf("case %d: Error(): want %q, got %q", i, want, got)
		}
	}
}

func TestConflictIs(t *testing.T) {
	defaultConflict := Conflict{
		Type: ConflictModifiedFileMissing,
		File: "path/to/file.txt",
	}

	for name, tc := range map[string]struct {
		Conflict Conflict
		Target   error
		Match    bool
	}{
		"nil": {
			Conflict: defaultConflict,
			Target:   nil,
			Match:    false,
		},
		"otherType": {
			Conflict: defaultConflict,
			Target:   errors.New("different error"),
			Match:    false,
		},
		"emptyConflictMatches": {
			Conflict: defaultConflict,
			Target:   &Conflict{},
			Match:    true,
		},
		"typeOnlyMatch": {
			Conflict: defaultConflict,
			Target:   &Conflict{Type: ConflictModifiedFileMissing},
			Match:    true,
		},
		"fileOnlyMatch": {
			Conflict: defaultConflict,
			Target:   &Conflict{File: "path/to/file.txt"},
			Match:    true,
		},
		"typeAndFileMatch": {
			Conflict: defaultConflict,
			Target:   &Conflict{Type: ConflictModifiedFileMissing, File: "path/to/file.txt"},
			Match:    true,
		},
		"differentType": {
			Conflict: defaultConflict,
			Target:   &Conflict{Type: ConflictContent},
			Match:    false,
		},
		"differentFile": {
			Conflict: defaultConflict,
			Target:   &Conflict{File: "path/to/other/file.txt"},
			Match:    false,
		},
	} {
		want := tc.Match
		t.Run(name, func(t *testing.T) {
			if got := tc.Conflict.Is(tc.Target); got != want {
				t.Errorf("Is(target): want %t, got %t", want, got)
			}
		})
	}
}
