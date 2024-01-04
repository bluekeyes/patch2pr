package patch2pr

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"github.com/google/go-github/v57/github"
	"github.com/shurcooL/githubv4"
)

const (
	defaultMode = os.FileMode(0o100644)
)

// GraphQLApplier applies patches to create commits in a repository. Compared
// to the normal Applier, the GraphqQLApplier:
//
//   - Generally makes fewer API requests
//   - Does not support setting a commit author or committer
//   - Does not create intermediate blobs and trees
//   - Uses more memory while applying patches with multiple files
//   - Updates a branch (which must exist) to reference the new commit
//   - Creates signed commits
//
// Due to limitations in the GraphQL API, not all patches are supported; see
// Apply. Use the regular Applier if you need to apply arbitrary patches or if
// you are targeting a GitHub Enterprise instance that does not support the
// createCommitOnBranch GraphQL mutation.
type GraphQLApplier struct {
	v4client *githubv4.Client
	v3client *github.Client
	owner    string
	repo     string

	commit    string
	changes   map[string]pendingChange
	modeCache map[string]os.FileMode
}

type pendingChange struct {
	IsDelete bool
	Content  []byte
}

// NewGraphQLApplier creates an applier for a repository. The applier applies
// changes on top of base, the full OID (SHA) of a commit.
func NewGraphQLApplier(client *githubv4.Client, repo Repository, base string) *GraphQLApplier {
	a := &GraphQLApplier{
		v4client: client,
		owner:    repo.Owner,
		repo:     repo.Name,
	}
	a.Reset(base)
	return a
}

// SetV3Client sets a REST API client used to implement functionality missing
// in the GraphQL API. Without a V3 client, the applier will fail on patches
// that modify binary and very large files.
func (a *GraphQLApplier) SetV3Client(client *github.Client) {
	a.v3client = client
}

// Apply applies the changes in a file, adding the result to the list of
// pending file changes. It does not modify the repository.
//
// Due to GraphQL limitations, some patches are not supported:
//
//   - Adding or renaming files that use a non-standard mode
//   - Changing the mode of an existing file
//   - Modifying or deleting binary files (without a V3 client)
//   - Modifying or deleting large files (without a V3 client)
//
// When given an unsupported patch, Apply returns an error such that
// IsUnsupported(err) is true. Setting a V3 client with SetV3Client allows
// Apply to process some patches that are otherwise unsupported.
func (a *GraphQLApplier) Apply(ctx context.Context, f *gitdiff.File) error {
	// As of 2021-09-22, createCommitOnBranch handles file modes
	// inconsistently:
	//
	//   1. All new files use 644
	//   2. It's not possible to change the mode of an existing file
	//   3. Modifying an existing file retains the existing mode
	//   4. Renaming a file converts it to 644 (modeled as a remove and add)
	//   5. Mode isn't relevant when removing a file
	//
	switch {
	case f.NewMode != 0 && f.NewMode != defaultMode:
		return unsupported("GraphQL cannot apply files with non-standard modes: %o", f.NewMode)
	case isModeChange(f.OldMode, f.NewMode):
		return unsupported("GraphQL cannot change file modes")
	case isRename(f):
		existingMode, err := a.getMode(ctx, f.OldName)
		if err != nil {
			return err
		}
		if existingMode != defaultMode {
			return unsupported("GraphQL cannot rename files with non-standard modes: %o", existingMode)
		}
	}

	switch {
	case f.IsNew:
		return a.applyCreate(ctx, f)
	case f.IsDelete:
		return a.applyDelete(ctx, f)
	default:
		return a.applyModify(ctx, f)
	}
}

func (a *GraphQLApplier) applyCreate(ctx context.Context, f *gitdiff.File) error {
	_, exists, err := a.getContent(ctx, f.NewName)
	if err != nil {
		return err
	}
	if exists {
		return errors.New("existing entry for new file")
	}

	var b bytes.Buffer
	if err := gitdiff.Apply(&b, bytes.NewReader(nil), f); err != nil {
		return err
	}

	a.changes[f.NewName] = pendingChange{Content: b.Bytes()}
	a.modeCache[f.NewName] = defaultMode
	return nil
}

func (a *GraphQLApplier) applyDelete(ctx context.Context, f *gitdiff.File) error {
	data, exists, err := a.getContent(ctx, f.OldName)
	if err != nil {
		return err
	}
	if !exists {
		// because the rest of application is strict, return an error if the
		// file was already deleted, since it indicates a conflict of some kind
		return errors.New("missing entry for deleted file")
	}

	if err := gitdiff.Apply(ioutil.Discard, bytes.NewReader(data), f); err != nil {
		return err
	}

	a.changes[f.OldName] = pendingChange{IsDelete: true}
	return nil
}

func (a *GraphQLApplier) applyModify(ctx context.Context, f *gitdiff.File) error {
	data, exists, err := a.getContent(ctx, f.OldName)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("no entry for modified file")
	}

	if len(f.TextFragments) > 0 || f.BinaryFragment != nil {
		var b bytes.Buffer
		if err := gitdiff.Apply(&b, bytes.NewReader(data), f); err != nil {
			return err
		}
		data = b.Bytes()
	}

	// delete the old file if it was removed
	if f.OldName != f.NewName {
		a.changes[f.OldName] = pendingChange{IsDelete: true}
	}

	a.changes[f.NewName] = pendingChange{Content: data}
	a.modeCache[f.NewName] = defaultMode
	return nil
}

func (a *GraphQLApplier) getContent(ctx context.Context, filePath string) ([]byte, bool, error) {
	if existing, ok := a.changes[filePath]; ok {
		if existing.IsDelete {
			return nil, false, nil
		}
		return existing.Content, true, nil
	}

	var q struct {
		Repository struct {
			Object struct {
				Blob struct {
					OID         string
					IsTruncated bool
					Text        *string
				} `graphql:"... on Blob"`
			} `graphql:"object(expression: $expr)"`
		} `graphql:"repository(owner: $owner, name: $name)"`
	}
	vars := map[string]interface{}{
		"owner": githubv4.String(a.owner),
		"name":  githubv4.String(a.repo),
		"expr":  githubv4.String(fmt.Sprintf("%s:%s", a.commit, filePath)),
	}

	if err := a.v4client.Query(ctx, &q, vars); err != nil {
		return nil, false, fmt.Errorf("repository blob query failed: %w", err)
	}

	blob := q.Repository.Object.Blob
	if blob.OID == "" {
		return nil, false, nil
	}
	if !blob.IsTruncated && blob.Text != nil {
		return []byte(*blob.Text), true, nil
	}

	// Either the file is binary or is too big for GraphQL, so fall back to the
	// REST API if a client is available
	if a.v3client == nil {
		return nil, true, unsupported("GraphQL cannot read the file content and there is no fallback v3 client")
	}

	b, _, err := a.v3client.Git.GetBlobRaw(ctx, a.owner, a.repo, blob.OID)
	if err != nil {
		return nil, true, fmt.Errorf("get blob failed: %w", err)
	}
	return b, true, nil
}

func (a *GraphQLApplier) getMode(ctx context.Context, filePath string) (os.FileMode, error) {
	if m, ok := a.modeCache[filePath]; ok {
		return m, nil
	}

	var q struct {
		Repository struct {
			Object struct {
				Tree struct {
					Entries []struct {
						Type string
						Path string
						Mode int
					}
				} `graphql:"... on Tree"`
			} `graphql:"object(expression: $expr)"`
		} `graphql:"repository(owner: $owner, name: $name)"`
	}
	vars := map[string]interface{}{
		"owner": githubv4.String(a.owner),
		"name":  githubv4.String(a.repo),
		"expr":  githubv4.String(fmt.Sprintf("%s:%s", a.commit, treePath(filePath))),
	}

	if err := a.v4client.Query(ctx, &q, vars); err != nil {
		return 0, fmt.Errorf("repository tree query failed: %w", err)
	}

	for _, entry := range q.Repository.Object.Tree.Entries {
		if entry.Type == "blob" {
			a.modeCache[entry.Path] = os.FileMode(entry.Mode)
		}
	}

	if m, ok := a.modeCache[filePath]; ok {
		return m, nil
	}
	return 0, fmt.Errorf("file did not appear in tree entries: %s", filePath)
}

// Commit creates a commit with all pending file changes. It updates the branch
// ref to point at the new commit and returns the OID (SHA) of the commit. The
// branch must already exist and reference at the current base commit of the
// GraphQLApplier.
//
// If header is not nil, Apply uses it to set the commit message. It ignores
// other fields set in header. In particular, the commit timestamp, author, and
// committer are always set by GitHub.
func (a *GraphQLApplier) Commit(ctx context.Context, ref string, header *gitdiff.PatchHeader) (string, error) {
	if len(a.changes) == 0 {
		return "", fmt.Errorf("no pending file changes")
	}

	var m struct {
		CreateCommitOnBranch struct {
			Commit struct {
				OID string
			}
		} `graphql:"createCommitOnBranch(input: $input)"`
	}

	input := a.makeInput(ref, header)
	if err := a.v4client.Mutate(ctx, &m, input, nil); err != nil {
		return "", fmt.Errorf("commit failed: %w", err)
	}

	oid := m.CreateCommitOnBranch.Commit.OID
	a.commit = oid
	a.changes = make(map[string]pendingChange)

	return oid, nil
}

func (a *GraphQLApplier) makeInput(ref string, header *gitdiff.PatchHeader) githubv4.CreateCommitOnBranchInput {
	branch := githubv4.String(ref)
	repoNameWithOwner := githubv4.String(fmt.Sprintf("%s/%s", a.owner, a.repo))

	input := githubv4.CreateCommitOnBranchInput{
		Branch: githubv4.CommittableBranch{
			BranchName:              &branch,
			RepositoryNameWithOwner: &repoNameWithOwner,
		},
		ExpectedHeadOid: githubv4.GitObjectID(a.commit),
		Message: githubv4.CommitMessage{
			Headline: githubv4.String(DefaultCommitMessage),
		},
		FileChanges: &githubv4.FileChanges{},
	}

	if header != nil {
		input.Message.Headline = githubv4.String(header.Title)
		if header.Body != "" {
			body := githubv4.String(header.Body)
			input.Message.Body = &body
		}
	}

	var dels []githubv4.FileDeletion
	var adds []githubv4.FileAddition
	for path, change := range a.changes {
		switch {
		case change.IsDelete:
			dels = append(dels, githubv4.FileDeletion{
				Path: githubv4.String(path),
			})
		default:
			content := base64.StdEncoding.EncodeToString(change.Content)
			adds = append(adds, githubv4.FileAddition{
				Path:     githubv4.String(path),
				Contents: githubv4.Base64String(content),
			})
		}
	}
	if len(dels) > 0 {
		input.FileChanges.Deletions = &dels
	}
	if len(adds) > 0 {
		input.FileChanges.Additions = &adds
	}

	return input
}

// Reset resets the applier so that future Apply calls start from commit base.
// It removes all pending file changes. Reset does not modify the repository.
func (a *GraphQLApplier) Reset(base string) {
	a.commit = base
	a.changes = make(map[string]pendingChange)
	a.modeCache = make(map[string]os.FileMode)
}

func isModeChange(m1, m2 os.FileMode) bool {
	return m1 != 0 && m2 != 0 && m1 != m2
}

func isRename(f *gitdiff.File) bool {
	if f.IsRename {
		return true
	}
	return f.OldName != "" && f.NewName != "" && f.OldName != f.NewName
}

func treePath(filePath string) string {
	tp := path.Dir(filePath)
	if tp == "." {
		return ""
	}
	return tp
}
