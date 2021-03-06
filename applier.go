package patch2pr

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"
	"time"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"github.com/google/go-github/v33/github"
)

// DefaultCommitMessage is the commit message used when no message is provided
// in a patch header.
var DefaultCommitMessage = "Apply patch with patch2pr"

// Applier applies patches to create trees and commits in a repository.
type Applier struct {
	client *github.Client
	owner  string
	repo   string

	commit      *github.Commit
	tree        string
	treeCache   map[string]*github.Tree
	entries     []*github.TreeEntry
	uncommitted bool
}

// NewApplier creates a new Applier for a repository. The Applier applies
// changes on top of commit c.
func NewApplier(client *github.Client, repo Repository, c *github.Commit) *Applier {
	a := &Applier{
		client: client,
		owner:  repo.Owner,
		repo:   repo.Name,
	}
	a.Reset(c)
	return a
}

// Apply applies the changes in a file, adds the result to the list of pending
// tree entries, and returns the entry. If the application succeeds, Apply
// creates a blob in the repository with the modified content.
func (a *Applier) Apply(ctx context.Context, f *gitdiff.File) (*github.TreeEntry, error) {
	// TODO(bkeyes): validate file to make sure fields are consistent
	// maybe two modes: validate and fix, where fix tries to set
	// missing fields based on the framents or the set fields
	//
	// in particular, we need IsNew, IsDelete, maybe IsCopy and IsRename to
	// agree with the fragments and NewName/OldName

	// Because of the tree cache, files must be part of a remote (i.e. created)
	// tree before they are modifiable. This could be fixed by refactoring the
	// apply logic but doesn't seem like an onerous restriction.
	for _, e := range a.entries {
		if e.GetPath() == f.NewName {
			return nil, errors.New("cannot apply with pending changes to file")
		}
	}

	var err error
	switch {
	case f.IsNew:
		err = a.applyCreate(ctx, f)
	case f.IsDelete:
		err = a.applyDelete(ctx, f)
	default:
		err = a.applyModify(ctx, f)
	}
	if err != nil {
		return nil, err
	}

	entry := a.entries[len(a.entries)-1]

	if entry.Content != nil {
		blob, _, err := a.client.Git.CreateBlob(ctx, a.owner, a.repo, &github.Blob{
			Content:  entry.Content,
			Encoding: github.String("base64"),
		})
		if err != nil {
			return nil, fmt.Errorf("create blob failed: %w", err)
		}
		entry.SHA = blob.SHA
		entry.Content = nil
	}

	return entry, nil
}

func (a *Applier) applyCreate(ctx context.Context, f *gitdiff.File) error {
	_, exists, err := a.getEntry(ctx, f.NewName)
	if err != nil {
		return err
	}
	if exists {
		return errors.New("existing entry for new file")
	}

	c, err := base64Apply(nil, f)
	if err != nil {
		return err
	}

	a.entries = append(a.entries, &github.TreeEntry{
		Path:    github.String(f.NewName),
		Mode:    github.String(getMode(f, nil)),
		Type:    github.String("blob"),
		Content: &c,
	})
	return nil
}

func (a *Applier) applyDelete(ctx context.Context, f *gitdiff.File) error {
	entry, exists, err := a.getEntry(ctx, f.OldName)
	if err != nil {
		return err
	}
	if !exists {
		// because the rest of application is strict, return an error if the
		// file was already deleted, since it indicates a conflict of some kind
		return errors.New("missing entry for deleted file")
	}

	data, _, err := a.client.Git.GetBlobRaw(ctx, a.owner, a.repo, entry.GetSHA())
	if err != nil {
		return fmt.Errorf("get blob content failed: %w", err)
	}

	if err := gitdiff.Apply(ioutil.Discard, bytes.NewReader(data), f); err != nil {
		return err
	}

	a.entries = append(a.entries, &github.TreeEntry{
		Path: github.String(f.OldName),
		Mode: entry.Mode,
	})
	return nil
}

func (a *Applier) applyModify(ctx context.Context, f *gitdiff.File) error {
	entry, exists, err := a.getEntry(ctx, f.OldName)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("no entry for modified file")
	}

	newEntry := &github.TreeEntry{
		Path: github.String(f.NewName),
		Mode: github.String(getMode(f, entry)),
		Type: github.String("blob"),
	}

	if len(f.TextFragments) > 0 || f.BinaryFragment != nil {
		data, _, err := a.client.Git.GetBlobRaw(ctx, a.owner, a.repo, entry.GetSHA())
		if err != nil {
			return fmt.Errorf("get blob content failed: %w", err)
		}

		c, err := base64Apply(data, f)
		if err != nil {
			return err
		}
		newEntry.Content = &c
	}

	// delete the old file if it was renamed
	if f.OldName != f.NewName {
		a.entries = append(a.entries, &github.TreeEntry{
			Path: github.String(f.OldName),
			Mode: entry.Mode,
		})
	}

	if newEntry.Content == nil {
		newEntry.SHA = entry.SHA
	}
	a.entries = append(a.entries, newEntry)

	return nil
}

// Entries returns the list of pending tree entries.
func (a *Applier) Entries() []*github.TreeEntry {
	return a.entries
}

// CreateTree creates a tree from the pending tree entries and clears the entry
// list. The new tree serves as the base tree for future Apply calls.
// CreateTree returns an error if there are no pending tree entires.
func (a *Applier) CreateTree(ctx context.Context) (*github.Tree, error) {
	if len(a.entries) == 0 {
		return nil, errors.New("no pending tree entries")
	}

	tree, _, err := a.client.Git.CreateTree(ctx, a.owner, a.repo, a.tree, a.entries)
	if err != nil {
		return nil, err
	}

	// Clear the tree cache here because we only cache trees that lead to file
	// modifications, and any change creates a new (i.e. uncached) tree SHA
	a.treeCache = make(map[string]*github.Tree)
	a.tree = tree.GetSHA()
	a.entries = nil
	a.uncommitted = true
	return tree, nil
}

// Commit commits the latest tree, optionally using the details in header. If
// there are pending tree entries, it calls CreateTree before creating the
// commit. If header is nil or missing fields, Commit uses a default message,
// the current time, and the authenticated user as needed for the commit
// details. Commit returns an error if there are no pending trees or tree
// entries.
func (a *Applier) Commit(ctx context.Context, header *gitdiff.PatchHeader) (*github.Commit, error) {
	if !a.uncommitted && len(a.entries) == 0 {
		return nil, errors.New("no pending tree or tree entries")
	}
	if len(a.entries) > 0 {
		if _, err := a.CreateTree(ctx); err != nil {
			return nil, fmt.Errorf("create tree failed: %w", err)
		}
	}

	c := &github.Commit{
		Tree: &github.Tree{
			SHA: github.String(a.tree),
		},
		Parents: []*github.Commit{
			a.commit,
		},
	}

	if header != nil {
		c.Message = github.String(header.Message())
		c.Author = makeCommitAuthor(header.Author, header.AuthorDate)
		c.Committer = makeCommitAuthor(header.Committer, header.CommitterDate)
	}
	if c.Message == nil || *c.Message == "" {
		c.Message = github.String("Apply patch with patch2pr")
	}

	commit, _, err := a.client.Git.CreateCommit(ctx, a.owner, a.repo, c)
	if err != nil {
		return nil, err
	}

	a.commit = commit
	a.uncommitted = false
	return commit, nil
}

// Reset resets the applier so that future Apply calls start from commit c. It
// removes pending tree entries and clears the latest tree. Reset does not
// modify the remote repository.
func (a *Applier) Reset(c *github.Commit) {
	a.commit = c
	a.tree = c.GetTree().GetSHA()
	a.treeCache = make(map[string]*github.Tree)
	a.entries = nil
	a.uncommitted = false
}

// getEntry returns the tree entry for path in the base tree. Returns nil and
// false if no entry exists for path.
func (a *Applier) getEntry(ctx context.Context, path string) (*github.TreeEntry, bool, error) {
	parts := strings.Split(path, "/")
	dir, name := parts[:len(parts)-1], parts[len(parts)-1]

	tree, err := a.getTree(ctx, a.tree)
	if err != nil {
		return nil, false, err
	}

	for _, s := range dir {
		entry, ok := findTreeEntry(tree, s, "tree")
		if !ok {
			return nil, false, nil
		}

		tree, err = a.getTree(ctx, entry.GetSHA())
		if err != nil {
			return nil, false, err
		}
	}

	entry, ok := findTreeEntry(tree, name, "blob")
	return entry, ok, nil
}

func (a *Applier) getTree(ctx context.Context, sha string) (*github.Tree, error) {
	if tree, ok := a.treeCache[sha]; ok {
		return tree, nil
	}

	tree, _, err := a.client.Git.GetTree(ctx, a.owner, a.repo, sha, false)
	if err != nil {
		return nil, fmt.Errorf("get tree %s failed: %w", sha, err)
	}
	a.treeCache[sha] = tree
	return tree, nil
}

func findTreeEntry(t *github.Tree, name, entryType string) (*github.TreeEntry, bool) {
	for _, entry := range t.Entries {
		if entry.GetPath() == name && entry.GetType() == entryType {
			return entry, true
		}
	}
	return nil, false
}

func base64Apply(data []byte, f *gitdiff.File) (string, error) {
	var b bytes.Buffer

	enc := base64.NewEncoder(base64.StdEncoding, &b)
	if err := gitdiff.Apply(enc, bytes.NewReader(data), f); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("base64 encoding failed: %w", err)
	}

	return b.String(), nil
}

// TODO(bkeyes): extract this to go-gitdiff in some form?
func getMode(f *gitdiff.File, existing *github.TreeEntry) string {
	switch {
	case f.NewMode > 0:
		return strconv.FormatInt(int64(f.NewMode), 8)
	case existing != nil && existing.GetMode() != "":
		return existing.GetMode()
	case f.OldMode > 0:
		return strconv.FormatInt(int64(f.OldMode), 8)
	}
	return "100644"
}

func makeCommitAuthor(id *gitdiff.PatchIdentity, d time.Time) *github.CommitAuthor {
	if id == nil && d.IsZero() {
		return nil
	}

	a := &github.CommitAuthor{}
	if id != nil {
		if id.Name != "" {
			a.Name = github.String(id.Name)
		}
		if id.Email != "" {
			a.Email = github.String(id.Email)
		}
	}
	if !d.IsZero() {
		a.Date = &d
	}
	return a
}
