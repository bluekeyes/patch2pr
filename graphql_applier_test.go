package patch2pr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
)

// GraphQLSkipPatches contains the names of patches that cannot be applied
// by the GraphQLApplier due to API limitation.
var GraphQLSkipPatches = map[string]bool{
	"changeToSymlink": true,
	"modeChange":      true,

	// TODO(bkeyes): This fails because the test file is executable, but fails
	// post-apply, not on validation. Make sure we reject the patch and also
	// add a test with a normal file to make sure rename work.
	"renameFile": true,
}

func TestGraphQLApplier(t *testing.T) {
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
			if GraphQLSkipPatches[name] {
				t.Skip("incompatible with GraphQL applier")
			}
			runGraphQLPatchTest(t, tctx, name)
		})
	}
}

func runGraphQLPatchTest(t *testing.T, tctx *TestContext, name string) {
	f, err := os.Open(filepath.Join("testdata", "patches", name+".patch"))
	if err != nil {
		t.Fatalf("error opening patch file: %v", err)
	}
	defer f.Close()

	files, _, err := gitdiff.Parse(f)
	if err != nil {
		t.Fatalf("error parsing patch: %v", err)
	}

	applier := NewGraphQLApplier(tctx.V4Client, tctx.Repo, tctx.BaseCommit.GetSHA())
	applier.SetV3Client(tctx.Client)

	for _, file := range files {
		if err := applier.Apply(tctx, file); err != nil {
			t.Fatalf("error applying file patch: %s: %v", file.NewName, err)
		}
	}

	// GraphQL applies require that the target branch already exists
	ref := NewReference(tctx.Client, tctx.Repo, tctx.Branch(name))
	if err := ref.Set(tctx, tctx.BaseCommit.GetSHA(), true); err != nil {
		t.Fatalf("error creating ref: %v", err)
	}

	sha, err := applier.Commit(tctx, tctx.Branch(name), &gitdiff.PatchHeader{
		Title: name,
	})
	if err != nil {
		t.Fatalf("error committing changes: %v", err)
	}

	commit, _, err := tctx.Client.Git.GetCommit(tctx, tctx.Repo.Owner, tctx.Repo.Name, sha)
	if err != nil {
		t.Fatalf("error getting new commit: %v", err)
	}

	assertPatchResult(t, tctx, name, commit)
}
