package patch2pr

import (
	"fmt"
	"strings"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
)

// Conflict is the error returned when applying a patch fails because of a
// conflict between the patch and the target.
type Conflict struct {
	// The type of the conflict.
	Type ConflictType
	// The path to the file in which the conflict occurs.
	File string
	// The line number of the conflict, if known.
	Line int64

	cause *gitdiff.Conflict
}

// ConflictType identifies the type of conflict.
type ConflictType int

const (
	// ConflictUnspecified indicates the cause of the conflict was not specified.
	ConflictUnspecified ConflictType = iota

	// ConflictNewFileExists indicates the patch creates a new file that already exists.
	ConflictNewFileExists

	// ConflictDeletedFileMissing indicates the patch deletes a file that does not exist.
	ConflictDeletedFileMissing

	// ConflictModifiedFileMissing indicates the patch modifies a file that does not exist.
	ConflictModifiedFileMissing

	// ConflictContent indicates the patch content does not apply cleanly against the file's content.
	ConflictContent
)

func (c *Conflict) Error() string {
	var msg strings.Builder
	if c.File != "" {
		msg.WriteString(c.File)
		if c.Line > 0 {
			fmt.Fprintf(&msg, ":%d", c.Line)
		}
		msg.WriteString(": ")
	}

	switch c.Type {
	case ConflictNewFileExists:
		msg.WriteString("conflict: new file already exists")
	case ConflictDeletedFileMissing:
		msg.WriteString("conflict: deleted file does not exist")
	case ConflictModifiedFileMissing:
		msg.WriteString("conflict: modified file does not exist")
	case ConflictContent:
		if c.cause != nil {
			msg.WriteString(c.cause.Error())
		} else {
			msg.WriteString("conflict: content")
		}
	default:
		msg.WriteString("conflict")
	}

	return msg.String()
}

// Is returns true if all of the non-zero fields of target equal the values of
// this Conflict. Passing an empty *Conflict{} always returns true.
func (c *Conflict) Is(target error) bool {
	if other, ok := target.(*Conflict); ok {
		if other.Type == ConflictUnspecified {
			if other.File == "" {
				return true
			}
			return c.File == other.File
		}
		if other.File == "" {
			return c.Type == other.Type
		}
		return other.Type == c.Type && other.File == c.File
	}
	return false
}

func (c *Conflict) Unwrap() error {
	return c.cause
}
