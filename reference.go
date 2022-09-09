package patch2pr

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v47/github"
)

// Reference is a named reference in a repository.
type Reference struct {
	client *github.Client
	owner  string
	repo   string
	ref    string
}

// NewReference creates a new Reference for ref in repo.
func NewReference(client *github.Client, repo Repository, ref string) *Reference {
	if !strings.HasPrefix(ref, "refs/") {
		ref = fmt.Sprintf("refs/%s", ref)
	}

	return &Reference{
		client: client,
		owner:  repo.Owner,
		repo:   repo.Name,
		ref:    ref,
	}
}

// Set creates or updates the reference to point to sha. If force is true and
// the reference exists, Set updates it even if the update is not a
// fast-forward.
func (r *Reference) Set(ctx context.Context, sha string, force bool) error {
	// Test for existence because update and create return 422 responses if the
	// the ref is missing or exists, respectively. The same code is also used
	// for other errors like passing a bad SHA, so our only other option is to
	// parse the string message, which is fragile.
	var exists bool
	if _, _, err := r.client.Git.GetRef(ctx, r.owner, r.repo, r.ref); err != nil {
		if rerr, ok := err.(*github.ErrorResponse); !ok || rerr.Response.StatusCode != 404 {
			return fmt.Errorf("get ref failed: %w", err)
		}
	} else {
		exists = true
	}

	if exists {
		if _, _, err := r.client.Git.UpdateRef(ctx, r.owner, r.repo, &github.Reference{
			Ref: github.String(r.ref),
			Object: &github.GitObject{
				SHA: github.String(sha),
			},
		}, force); err != nil {
			return fmt.Errorf("update ref failed: %w", err)
		}
	} else {
		if _, _, err := r.client.Git.CreateRef(ctx, r.owner, r.repo, &github.Reference{
			Ref: github.String(r.ref),
			Object: &github.GitObject{
				SHA: github.String(sha),
			},
		}); err != nil {
			return fmt.Errorf("create ref failed: %w", err)
		}
	}

	return nil
}

// PullRequest create a new pull request for the reference. The reference must
// be a branch (start with "refs/heads/".) The pull request takes values from
// spec, except for Head, which is set to the reference.
func (r *Reference) PullRequest(ctx context.Context, spec *github.NewPullRequest) (*github.PullRequest, error) {
	if !strings.HasPrefix(r.ref, "refs/heads/") {
		return nil, fmt.Errorf("reference %s is not a branch", r.ref)
	}

	specCopy := *spec
	specCopy.Head = github.String(strings.TrimPrefix(r.ref, "refs/heads/"))

	pr, _, err := r.client.PullRequests.Create(ctx, r.owner, r.repo, &specCopy)
	if err != nil {
		return nil, err
	}
	return pr, nil
}
