package patch2pr

import (
	"errors"
	"fmt"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
)

var (
	ErrConflict = errors.New("conflict prevented applying patch")

	ErrNewFileAlreadyExists = fmt.Errorf("%w: existing entry for new file", ErrConflict)
	ErrNoSuchFileToDelete   = fmt.Errorf("%w: missing entry for deleted file", ErrConflict)
	ErrNoSuchFileToModify   = fmt.Errorf("%w: no entry for modified file", ErrConflict)
)

func wrapGitdiffApplyError(err error) error {
	if errors.Is(err, &gitdiff.Conflict{}) {
		return fmt.Errorf("%w: gitdiff apply failed: %w", ErrConflict, err)
	}
	return fmt.Errorf("gitdiff apply failed: %w", err)
}

func unsupported(msg string, args ...any) error {
	return unsupportedErr{reason: fmt.Sprintf(msg, args...)}
}

type unsupportedErr struct {
	reason string
}

func (err unsupportedErr) Error() string {
	return fmt.Sprintf("unsupported: %s", err.reason)
}

func (err unsupportedErr) Unsupported() bool {
	return true
}

// IsUnsupported returns true if err is the result of trying an unsupported
// operation. It is equivalent to finding the first error in err's chain that
// implements
//
//	type unsupported interface {
//	    Unsupported() bool
//	}
//
// and then calling the Unsupported() method.
func IsUnsupported(err error) bool {
	var u interface {
		Unsupported() bool
	}
	return errors.As(err, &u) && u.Unsupported()
}
