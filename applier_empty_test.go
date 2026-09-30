package patch2pr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"github.com/google/go-github/v92/github"
)

func TestApplierEmptyCommitDisabled(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings []bool
	}{
		{name: "default"},
		{name: "explicitly disabled", settings: []bool{false}},
		{name: "disabled after enabling", settings: []bool{true, false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := &github.Commit{
				SHA:  new("base"),
				Tree: &github.Tree{SHA: new("tree")},
			}
			applier := NewApplier(nil, Repository{Owner: "owner", Name: "repo"}, base)
			for _, enabled := range tc.settings {
				applier.SetAllowEmptyCommits(enabled)
			}
			commit, err := applier.Commit(context.Background(), nil, nil)
			if err == nil || err.Error() != "no pending tree or tree entries" {
				t.Fatalf("expected empty commit error, got commit %v, error %v", commit, err)
			}
			if commit != nil {
				t.Fatalf("expected no commit, got %v", commit)
			}
		})
	}
}

func TestApplierEmptyCommits(t *testing.T) {
	parent, tree := "base", "base-tree"
	requests := 0
	date := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	header := &gitdiff.PatchHeader{
		Title:         "Check failed",
		Body:          "[skip ci]",
		Author:        &gitdiff.PatchIdentity{Name: "Author", Email: "author@example.com"},
		AuthorDate:    date,
		Committer:     &gitdiff.PatchIdentity{Name: "Committer", Email: "committer@example.com"},
		CommitterDate: date.Add(time.Minute),
	}
	tmpl := &github.Commit{
		Verification: &github.SignatureVerification{Signature: new("test-signature")},
	}

	transport := emptyCommitTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/repos/owner/repo/git/commits" {
			return nil, fmt.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		defer r.Body.Close()

		var got map[string]any
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			return nil, err
		}
		want := map[string]any{
			"tree":      tree,
			"parents":   []any{parent},
			"message":   "Check failed\n\n[skip ci]",
			"signature": "test-signature",
			"author": map[string]any{
				"name":  "Author",
				"email": "author@example.com",
				"date":  date.Format(time.RFC3339),
			},
			"committer": map[string]any{
				"name":  "Committer",
				"email": "committer@example.com",
				"date":  date.Add(time.Minute).Format(time.RFC3339),
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("unexpected commit request:\n got: %#v\nwant: %#v", got, want)
		}

		parent = fmt.Sprintf("commit-%d", requests)
		body, err := json.Marshal(&github.Commit{
			SHA:  new(parent),
			Tree: &github.Tree{SHA: new(tree)},
		})
		if err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusCreated,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(string(body))),
			Request:    r,
		}, nil
	})
	client, err := github.NewClient(github.WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	base := &github.Commit{SHA: new(parent), Tree: &github.Tree{SHA: new(tree)}}
	applier := NewApplier(client, Repository{Owner: "owner", Name: "repo"}, base)
	applier.SetAllowEmptyCommits(true)

	for i := 0; i < 3; i++ {
		if i == 2 {
			// Reset changes the parent and tree, but preserves the opt-in.
			parent, tree = "reset-base", "reset-tree"
			applier.Reset(&github.Commit{SHA: new(parent), Tree: &github.Tree{SHA: new(tree)}})
		}
		commit, err := applier.Commit(context.Background(), tmpl, header)
		if err != nil {
			t.Fatalf("commit %d failed: %v", i+1, err)
		}
		if commit.GetSHA() != parent || commit.GetTree().GetSHA() != tree {
			t.Fatalf("unexpected commit result: %v", commit)
		}
	}

	// Allowing empty commits does not allow creating a tree with no entries.
	if _, err := applier.CreateTree(context.Background()); err == nil {
		t.Error("expected an error creating a tree with no entries")
	}
	applier.SetAllowEmptyCommits(false)
	if _, err := applier.Commit(context.Background(), nil, nil); err == nil {
		t.Error("expected empty commit rejection after disabling")
	}
	if requests != 3 {
		t.Errorf("expected exactly three commit requests, got %d", requests)
	}
}

type emptyCommitTransport func(*http.Request) (*http.Response, error)

func (f emptyCommitTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
