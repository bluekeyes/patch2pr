// Package patch2pr converts Git patches in to GitHub pull requests.
package patch2pr

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"github.com/google/go-github/v88/github"
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
	entries     map[string]*github.TreeEntry
	uncommitted bool

	applyOptions []gitdiff.ApplyOption
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

// SetApplyOptions sets the options to use when calling [gitdiff.Apply]. Pass
// an empty list to remove previously set options.
func (a *Applier) SetApplyOptions(opts ...gitdiff.ApplyOption) {
	a.applyOptions = opts
}

// Apply applies the changes in a file, adds the result to the list of pending
// tree entries, and returns the entry. If the application succeeds, Apply
// creates a blob in the repository with the modified content.
//
// If the apply fails due to a conflict, Apply returns an error of type
// *Conflict.
func (a *Applier) Apply(ctx context.Context, f *gitdiff.File) (*github.TreeEntry, error) {
	// TODO(bkeyes): validate file to make sure fields are consistent
	// maybe two modes: validate and fix, where fix tries to set
	// missing fields based on the framents or the set fields
	//
	// in particular, we need IsNew, IsDelete, maybe IsCopy and IsRename to
	// agree with the fragments and NewName/OldName

	var entry *github.TreeEntry
	var err error
	switch {
	case f.IsNew:
		entry, err = a.applyCreate(ctx, f)
	case f.IsDelete:
		entry, err = a.applyDelete(ctx, f)
	default:
		entry, err = a.applyModify(ctx, f)
	}
	if err != nil {
		return nil, err
	}

	if entry.Content != nil {
		blob, _, err := a.client.Git.CreateBlob(ctx, a.owner, a.repo, github.Blob{
			Content:  entry.Content,
			Encoding: github.Ptr("base64"),
		})
		if err != nil {
			return nil, fmt.Errorf("create blob failed: %w", err)
		}
		entry.SHA = blob.SHA
		entry.Content = nil
	}

	return entry, nil
}

func (a *Applier) applyCreate(ctx context.Context, f *gitdiff.File) (*github.TreeEntry, error) {
	_, exists, err := a.getEntry(ctx, f.NewName)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, &Conflict{Type: ConflictNewFileExists, File: f.NewName}
	}

	c, err := base64Apply(nil, f.NewName, f, a.applyOptions...)
	if err != nil {
		return nil, err
	}

	path := f.NewName
	newEntry := &github.TreeEntry{
		Path:    &path,
		Mode:    github.Ptr(getMode(f, nil)),
		Type:    github.Ptr("blob"),
		Content: &c,
	}
	a.entries[path] = newEntry

	return newEntry, nil
}

func (a *Applier) applyDelete(ctx context.Context, f *gitdiff.File) (*github.TreeEntry, error) {
	entry, exists, err := a.getEntry(ctx, f.OldName)
	if err != nil {
		return nil, err
	}
	if !exists {
		// Because the rest of application is strict, return an error if the
		// file was already deleted, since it indicates a conflict of some kind
		return nil, &Conflict{Type: ConflictDeletedFileMissing, File: f.OldName}
	}

	data, _, err := a.client.Git.GetBlobRaw(ctx, a.owner, a.repo, entry.GetSHA())
	if err != nil {
		return nil, fmt.Errorf("get blob content failed: %w", err)
	}

	if err := apply(io.Discard, bytes.NewReader(data), f.OldName, f, a.applyOptions...); err != nil {
		return nil, err
	}

	path := f.OldName
	newEntry := &github.TreeEntry{
		Path: &path,
		Mode: entry.Mode,
	}
	a.entries[path] = newEntry

	return newEntry, nil
}

func (a *Applier) applyModify(ctx context.Context, f *gitdiff.File) (*github.TreeEntry, error) {
	entry, exists, err := a.getEntry(ctx, f.OldName)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, &Conflict{Type: ConflictModifiedFileMissing, File: f.OldName}
	}

	path := f.NewName
	newEntry := &github.TreeEntry{
		Path: &path,
		Mode: github.Ptr(getMode(f, entry)),
		Type: github.Ptr("blob"),
	}

	if len(f.TextFragments) > 0 || f.BinaryFragment != nil {
		data, _, err := a.client.Git.GetBlobRaw(ctx, a.owner, a.repo, entry.GetSHA())
		if err != nil {
			return nil, fmt.Errorf("get blob content failed: %w", err)
		}

		c, err := base64Apply(data, f.OldName, f, a.applyOptions...)
		if err != nil {
			return nil, err
		}
		newEntry.Content = &c
	}

	// delete the old file if it was renamed
	if f.OldName != f.NewName {
		path := f.OldName
		a.entries[path] = &github.TreeEntry{
			Path: &path,
			Mode: entry.Mode,
		}
	}

	if newEntry.Content == nil {
		newEntry.SHA = entry.SHA
	}
	a.entries[path] = newEntry

	return newEntry, nil
}

// Entries returns the list of pending tree entries.
func (a *Applier) Entries() []*github.TreeEntry {
	entries := make([]*github.TreeEntry, 0, len(a.entries))
	for _, e := range a.entries {
		entries = append(entries, e)
	}
	return entries
}

// CreateTree creates a tree from the pending tree entries and clears the entry
// list. The new tree serves as the base tree for future Apply calls.
// CreateTree returns an error if there are no pending tree entires.
func (a *Applier) CreateTree(ctx context.Context) (*github.Tree, error) {
	if len(a.entries) == 0 {
		return nil, errors.New("no pending tree entries")
	}

	tree, _, err := a.client.Git.CreateTree(ctx, a.owner, a.repo, a.tree, a.Entries())
	if err != nil {
		return nil, err
	}

	// Clear the tree cache here because we only cache trees that lead to file
	// modifications, and any change creates a new (i.e. uncached) tree SHA
	a.tree = tree.GetSHA()
	a.treeCache = make(map[string]*github.Tree)
	a.entries = make(map[string]*github.TreeEntry)
	a.uncommitted = true
	return tree, nil
}

// Commit commits the latest tree, optionally using the details in tmpl and
// header. If there are pending tree entries, it calls CreateTree before
// creating the commit. It returns an error if there are no pending trees or
// tree entries.
//
// If tmpl is not nil, Commit uses it as a template for the new commit,
// overwriting fields as needed. If header is not nil, Commit uses it to set
// the message, author, and committer for the new commit. Values in header
// overwrite those in tmpl.
//
// If both tmpl and header are nil or missing fields, Commit uses a default
// message, the current time, and the authenticated user as needed for the
// commit details.
func (a *Applier) Commit(ctx context.Context, tmpl *github.Commit, header *gitdiff.PatchHeader) (*github.Commit, error) {
	if !a.uncommitted && len(a.entries) == 0 {
		return nil, errors.New("no pending tree or tree entries")
	}
	if len(a.entries) > 0 {
		if _, err := a.CreateTree(ctx); err != nil {
			return nil, fmt.Errorf("create tree failed: %w", err)
		}
	}

	var c github.Commit
	if tmpl != nil {
		c = *tmpl
	}

	c.Tree = &github.Tree{
		SHA: github.Ptr(a.tree),
	}
	c.Parents = []*github.Commit{
		a.commit,
	}

	if header != nil {
		c.Message = github.Ptr(header.Message())
		c.Author = makeCommitAuthor(header.Author, header.AuthorDate)
		c.Committer = makeCommitAuthor(header.Committer, header.CommitterDate)
	}
	if c.Message == nil || *c.Message == "" {
		c.Message = github.Ptr("Apply patch with patch2pr")
	}

	commit, _, err := a.client.Git.CreateCommit(ctx, a.owner, a.repo, c, nil)
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
	a.entries = make(map[string]*github.TreeEntry)
	a.uncommitted = false
}

// getEntry returns the tree entry for a path. If the path has a pending
// change, return the entry representing that change, otherwise return an entry
// from the base tree. Returns nil and false if no entry exists for path.
func (a *Applier) getEntry(ctx context.Context, path string) (*github.TreeEntry, bool, error) {
	if entry, ok := a.entries[path]; ok {
		if entry.SHA == nil && entry.Content == nil {
			// The existing entry is a deletion, so pretend it doesn't exist
			return nil, false, nil
		}
		return entry, true, nil
	}

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

// base64Apply applies the patch in f to data and returns the result as a
// base64-encoded string.
func base64Apply(data []byte, name string, f *gitdiff.File, opts ...gitdiff.ApplyOption) (string, error) {
	var b bytes.Buffer

	enc := base64.NewEncoder(base64.StdEncoding, &b)
	if err := apply(enc, bytes.NewReader(data), name, f, opts...); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("base64 encoding failed: %w", err)
	}

	return b.String(), nil
}

// apply runs gitdiff.Apply, wrapping any conflicts in patch2pr's Conflict type.
func apply(dst io.Writer, src io.ReaderAt, name string, f *gitdiff.File, opts ...gitdiff.ApplyOption) error {
	if err := gitdiff.Apply(dst, src, f, opts...); err != nil {
		var applyErr *gitdiff.ApplyError
		var conflict *gitdiff.Conflict
		if errors.As(err, &applyErr) && errors.As(err, &conflict) {
			return &Conflict{
				Type:  ConflictContent,
				File:  name,
				Line:  applyErr.Line,
				cause: conflict,
			}
		}
		return err
	}
	return nil
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
			a.Name = github.Ptr(id.Name)
		}
		if id.Email != "" {
			a.Email = github.Ptr(id.Email)
		}
	}
	if !d.IsZero() {
		a.Date = &github.Timestamp{Time: d}
	}
	return a
}
