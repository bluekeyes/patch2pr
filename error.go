package patch2pr

import (
	"errors"
	"fmt"
)

func unsupported(msg string, args ...interface{}) error {
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
