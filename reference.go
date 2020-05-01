package patch2pr

import (
	"context"
	"strings"

	"github.com/google/go-github/v31/github"
)

// Reference is a named reference in a repository.
type Reference struct {
	client *github.Client
	owner  string
	repo   string
	ref    string
}

func NewReference(client *github.Client, repo Repository, ref string) *Reference {
	return &Reference{
		client: client,
		owner:  repo.Owner,
		repo:   repo.Name,
		ref:    strings.TrimPrefix(ref, "refs/"),
	}
}

// Set creates or updates the reference to point to sha. If force is true and
// the reference exists, Set updates it even if the update is not a
// fast-forward.
func (r *Reference) Set(ctx context.Context, sha string, force bool) error {
	panic("TODO(bkeyes): unimplemented")
}

// PullRequest create a new pull request for the reference. The pull request
// uses the values in spec, except for Head, which is set to the reference.
func (r *Reference) PullRequest(ctx context.Context, spec *github.NewPullRequest) (*github.PullRequest, error) {
	panic("TODO(bkeyes): unimplemented")
}
