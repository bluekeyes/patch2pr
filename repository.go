package patch2pr

import (
	"fmt"
	"strings"
)

// Repository identifies a GitHub repository.
type Repository struct {
	Owner string
	Name  string
}

func (r Repository) String() string {
	if r.Owner == "" && r.Name == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s", r.Owner, r.Name)
}

// ParseRepository parses a Repository from a string in "owner/name" format.
func ParseRepository(s string) (Repository, error) {
	i := strings.IndexByte(s, '/')
	if i < 0 {
		return Repository{}, fmt.Errorf("parse %q: missing slash", s)
	}
	if i == 0 || i == len(s)-1 {
		return Repository{}, fmt.Errorf("parse %q: missing owner or name", s)
	}
	return Repository{Owner: s[:i], Name: s[i+1:]}, nil
}
