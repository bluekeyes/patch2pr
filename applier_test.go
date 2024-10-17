package patch2pr

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"github.com/bluekeyes/patch2pr/internal"
	"github.com/google/go-github/v66/github"
	"github.com/shurcooL/githubv4"
)

const (
	BaseBranch = "base"
	DeletedExt = ".deleted"
)

const (
	EnvRepo  = "PATCH2PR_TEST_REPO"
	EnvToken = "PATCH2PR_TEST_GITHUB_TOKEN"
	EnvDebug = "PATCH2PR_TEST_DEBUG"
)

func TestApplier(t *testing.T) {
	tctx := prepareTestContext(t)

	t.Logf("Test ID: %s", tctx.ID)
	t.Logf("Test Repository: %s", tctx.Repo.String())

	createBranch(t, tctx)
	defer cleanupBranches(t, tctx)

	patches, err := filepath.Glob(filepath.Join("testdata", "patches", "*.patch"))
	if err != nil {
		t.Fatalf("error listing patches: %v", err)
	}

	t.Logf("Discovered %d patches", len(patches))
	for _, patch := range patches {
		name := strings.TrimSuffix(filepath.Base(patch), ".patch")
		t.Run(name, func(t *testing.T) {
			runPatchTest(t, tctx, name)
		})
	}
}

type TestContext struct {
	context.Context

	ID   string
	Repo Repository

	BaseCommit *github.Commit
	BaseTree   *github.Tree

	Client   *github.Client
	V4Client *githubv4.Client
}

func (tctx *TestContext) Branch(name string) string {
	return fmt.Sprintf("refs/heads/test/%s/%s", tctx.ID, name)
}

func runPatchTest(t *testing.T, tctx *TestContext, name string) {
	f, err := os.Open(filepath.Join("testdata", "patches", name+".patch"))
	if err != nil {
		t.Fatalf("error opening patch file: %v", err)
	}
	defer f.Close()

	files, _, err := gitdiff.Parse(f)
	if err != nil {
		t.Fatalf("error parsing patch: %v", err)
	}

	applier := NewApplier(tctx.Client, tctx.Repo, tctx.BaseCommit)
	for _, file := range files {
		if _, err := applier.Apply(tctx, file); err != nil {
			t.Fatalf("error applying file patch: %s: %v", file.NewName, err)
		}
	}

	commit, err := applier.Commit(tctx, nil, &gitdiff.PatchHeader{
		Title: name,
	})
	if err != nil {
		t.Fatalf("error committing changes: %v", err)
	}

	ref := NewReference(tctx.Client, tctx.Repo, tctx.Branch(name))
	if err := ref.Set(tctx, commit.GetSHA(), true); err != nil {
		t.Fatalf("error creating ref: %v", err)
	}

	assertPatchResult(t, tctx, name, commit)
}

type treeFile struct {
	Mode    string
	SHA     string
	Content []byte
}

func assertPatchResult(t *testing.T, tctx *TestContext, name string, c *github.Commit) {
	expected := entriesToMap(tctx.BaseTree.Entries)

	root := filepath.Join("testdata", "patches", name) + string(filepath.Separator)
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		relpath := filepath.ToSlash(strings.TrimPrefix(path, root))
		if strings.HasSuffix(relpath, DeletedExt) {
			delete(expected, strings.TrimSuffix(relpath, DeletedExt))
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		var content []byte
		if info.Mode()&fs.ModeSymlink > 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			content = []byte(target)
		} else {
			content, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}

		expected[relpath] = treeFile{
			Mode:    getGitMode(info),
			Content: content,
		}

		return nil
	}); err != nil {
		t.Fatalf("error listing expected files: %v", err)
	}

	actualTree, _, err := tctx.Client.Git.GetTree(tctx, tctx.Repo.Owner, tctx.Repo.Name, c.GetTree().GetSHA(), true)
	if err != nil {
		t.Fatalf("error getting actual tree: %v", err)
	}

	actual := entriesToMap(actualTree.Entries)
	for path, file := range actual {
		expectedFile, ok := expected[path]
		if !ok {
			t.Errorf("unexpected file %s", path)
			continue
		}
		delete(expected, path)

		if expectedFile.SHA != "" {
			if expectedFile.SHA != file.SHA {
				t.Errorf("unexpected modification to %s", path)
			}
			continue
		}

		if expectedFile.Mode != file.Mode {
			t.Errorf("incorrect mode: expected %s, actual %s: %s", expectedFile.Mode, file.Mode, path)
			continue
		}

		content, _, err := tctx.Client.Git.GetBlobRaw(tctx, tctx.Repo.Owner, tctx.Repo.Name, file.SHA)
		if err != nil {
			t.Fatalf("error getting blob content: %v", err)
		}
		if !bytes.Equal(expectedFile.Content, content) {
			t.Errorf("incorrect content: %s\nexpected: %q\n  actual: %q", path, expectedFile.Content, content)
		}
	}

	for path := range expected {
		t.Errorf("missing file %s", path)
	}
}

func entriesToMap(entries []*github.TreeEntry) map[string]treeFile {
	m := make(map[string]treeFile)
	for _, entry := range entries {
		if entry.GetType() != "blob" {
			continue
		}
		m[entry.GetPath()] = treeFile{
			Mode: entry.GetMode(),
			SHA:  entry.GetSHA(),
		}
	}
	return m
}

func getGitMode(info fs.FileInfo) string {
	if info.Mode()&fs.ModeSymlink > 0 {
		return "120000"
	}
	return fmt.Sprintf("100%o", info.Mode())
}

func prepareTestContext(t *testing.T) *TestContext {
	id := strconv.FormatInt(time.Now().UnixNano()/1000000, 10)

	fullRepo, ok := os.LookupEnv(EnvRepo)
	if !ok || fullRepo == "" {
		t.Skipf("%s must be set in the environment", EnvRepo)
	}
	token, ok := os.LookupEnv(EnvToken)
	if !ok || token == "" {
		t.Skipf("%s must be set in the environment", EnvToken)
	}

	repo, err := ParseRepository(fullRepo)
	if err != nil {
		t.Fatalf("Invalid %s value: %v", EnvRepo, err)
	}

	ctx := context.Background()
	httpClient := internal.NewTokenClient(token)

	tctx := TestContext{
		Context:  ctx,
		ID:       id,
		Repo:     repo,
		Client:   github.NewClient(httpClient),
		V4Client: githubv4.NewClient(httpClient),
	}
	return &tctx
}

func createBranch(t *testing.T, tctx *TestContext) {
	root := filepath.Join("testdata", "base") + string(filepath.Separator)

	var entries []*github.TreeEntry
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		treePath := strings.TrimPrefix(path, root)
		entry := github.TreeEntry{
			Path: &treePath,
			Type: github.String("blob"),
			Mode: github.String(getGitMode(info)),
		}

		if strings.HasSuffix(d.Name(), ".bin") {
			c := base64.StdEncoding.EncodeToString(content)
			blob, _, err := tctx.Client.Git.CreateBlob(tctx, tctx.Repo.Owner, tctx.Repo.Name, &github.Blob{
				Encoding: github.String("base64"),
				Content:  &c,
			})
			if err != nil {
				return err
			}
			entry.SHA = blob.SHA
		} else {
			c := string(content)
			entry.Content = &c
		}

		entries = append(entries, &entry)
		return nil
	}); err != nil {
		t.Fatalf("error listing base files: %v", err)
	}

	tree, _, err := tctx.Client.Git.CreateTree(tctx, tctx.Repo.Owner, tctx.Repo.Name, "", entries)
	if err != nil {
		t.Fatalf("error creating tree: %v", err)
	}

	fullTree, _, err := tctx.Client.Git.GetTree(tctx, tctx.Repo.Owner, tctx.Repo.Name, tree.GetSHA(), true)
	if err != nil {
		t.Fatalf("error getting recursive tree: %v", err)
	}

	commit := &github.Commit{
		Message: github.String("Base commit for test"),
		Tree:    tree,
	}
	commit, _, err = tctx.Client.Git.CreateCommit(tctx, tctx.Repo.Owner, tctx.Repo.Name, commit, nil)
	if err != nil {
		t.Fatalf("error creating commit: %v", err)
	}

	tctx.BaseCommit = commit
	tctx.BaseTree = fullTree

	if _, _, err := tctx.Client.Git.CreateRef(tctx, tctx.Repo.Owner, tctx.Repo.Name, &github.Reference{
		Ref: github.String(tctx.Branch(BaseBranch)),
		Object: &github.GitObject{
			SHA: commit.SHA,
		},
	}); err != nil {
		t.Fatalf("error creating ref: %v", err)
	}
}

func cleanupBranches(t *testing.T, tctx *TestContext) {
	if isDebug() && t.Failed() {
		t.Logf("Debug mode enabled with failing tests, skipping ref cleanup: %s", tctx.ID)
		return
	}

	refs, _, err := tctx.Client.Git.ListMatchingRefs(tctx, tctx.Repo.Owner, tctx.Repo.Name, &github.ReferenceListOptions{
		Ref: fmt.Sprintf("heads/test/%s/", tctx.ID),
	})
	if err != nil {
		t.Logf("WARNING: failed to list refs; skipping cleanup: %v", err)
		return
	}

	t.Logf("Found %d refs to remove", len(refs))
	for _, ref := range refs {
		t.Logf("Deleting %s", ref.GetRef())
		if _, err := tctx.Client.Git.DeleteRef(tctx, tctx.Repo.Owner, tctx.Repo.Name, ref.GetRef()); err != nil {
			t.Logf("WARNING: failed to delete ref %s: %v", ref.GetRef(), err)
		}
	}
}

func isDebug() bool {
	val := os.Getenv(EnvDebug)
	return val != ""
}
