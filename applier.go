package patch2pr

import (
	"context"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"github.com/google/go-github/v31/github"
)

type TreeSHA string

// Applier applies patches to create trees and commits in a repository.
type Applier struct {
	client     *github.Client
	owner      string
	repo       string
	eagerBlobs bool

	root      TreeSHA
	trees     map[TreeSHA]*github.Tree
	entries   []*github.TreeEntry
	committed bool
}

// NewApplier creates a new Applier for a repository. The Applier applies
// changes on top of the tree with SHA root.
func NewApplier(client *github.Client, repo Repository, root TreeSHA) *Applier {
	a := &Applier{
		client: client,
		owner:  repo.Owner,
		repo:   repo.Name,
	}
	a.Reset(root)
	return a
}

// Apply applies the changes in a file, adds the result to the list of pending
// tree entries, and returns the entry.
//
// If EagerBlobs is true, Apply creates a blob for any content and returns an
// entry with a blob reference. Otherwise, the repository is not modified and
// the entry contains the new content.
func (a *Applier) Apply(ctx context.Context, f *gitdiff.File) (*github.TreeEntry, error) {
	panic("TODO(bkeyes): unimplemented")
}

// SetEagerBlobs enables or disables eager blob creation in the applier. When
// enabled, blobs are created in the repository as soon as possible. When
// disabled, blobs are created only when creating a tree. This reduces the
// number of API requests but uses more memory.
func (a *Applier) SetEagerBlobs(on bool) {
	a.eagerBlobs = on
}

// Entries returns the list of pending tree entries.
func (a *Applier) Entries() []*github.TreeEntry {
	return a.entries
}

// CreateTree creates a tree from the pending tree entries and clears the entry
// list. The new tree serves as the root for future Apply calls.
func (a *Applier) CreateTree(ctx context.Context) (*github.Tree, error) {
	panic("TODO(bkeyes): unimplemented")
}

// Commit commits the latest tree, optionally using the details in header. If
// there are pending tree entries, it calls CreateTree before creating the
// commit. If header is nil, Commit uses a default message, the current time,
// and the authenticated user for the commit details.
func (a *Applier) Commit(ctx context.Context, header *gitdiff.PatchHeader) (*github.Commit, error) {
	panic("TODO(bkeyes): unimplemented")
}

// Reset resets the applier so that future Apply calls start from the tree with
// SHA root. It removes pending tree entries and clears the latest tree. It
// does not modify the remote repository.
func (a *Applier) Reset(root TreeSHA) {
	a.root = root
	a.trees = make(map[TreeSHA]*github.Tree)
	a.entries = nil
	a.committed = false
}
