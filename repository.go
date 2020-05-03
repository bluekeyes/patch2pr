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
	if i := strings.IndexByte(s, '/'); 1 <= i && i < len(s)-1 {
		return Repository{Owner: s[:i], Name: s[i+1:]}, nil
	}
	return Repository{}, fmt.Errorf("invalid repository: %s", s)
}
