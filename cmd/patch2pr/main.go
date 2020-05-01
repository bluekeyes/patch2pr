package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"github.com/google/go-github/v31/github"
	"golang.org/x/oauth2"
)

func die(code int, err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(code)
}

func main() {
	var baseBranch, headBranch, message, patchBase, pullTitle, repository, githubToken, githubURL string
	var force, outputJSON, noPullRequest bool

	fs := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	fs.SetOutput(ioutil.Discard)
	fs.Usage = func() {}

	fs.StringVar(&baseBranch, "base-branch", "", "base-branch")
	fs.BoolVar(&force, "force", false, "force")
	fs.StringVar(&headBranch, "head-branch", "patch2pr", "head-branch")
	fs.BoolVar(&outputJSON, "json", false, "json")
	fs.StringVar(&message, "message", "", "message")
	fs.BoolVar(&noPullRequest, "no-pull-request", false, "no-pull-request")
	fs.StringVar(&patchBase, "patch-base", "", "patch-base")
	fs.StringVar(&pullTitle, "pull-title", "", "pull-title")
	fs.StringVar(&repository, "repository", "", "repository")
	fs.StringVar(&githubToken, "token", "", "token")
	fs.StringVar(&githubURL, "url", "https://api.github.com", "url")

	if err := fs.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			fmt.Fprintln(os.Stdout, helpText())
			os.Exit(0)
		}
		die(2, err)
	}

	if repository == "" {
		die(2, errors.New("the -repository flag is required"))
	}
	parts := strings.Split(repository, "/")
	if len(parts) != 2 {
		die(2, fmt.Errorf("invalid -repository value: %s: repository must be in 'owner/name' format", repository))
	}
	owner, repo := parts[0], parts[1]

	u, err := url.Parse(githubURL)
	if err != nil {
		die(2, fmt.Errorf("invalid github URL: %s: %v", githubURL, err))
	}
	if !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}

	if githubToken == "" {
		githubToken = os.Getenv("GITHUB_TOKEN")
		if githubToken == "" {
			die(2, errors.New("a github token is required; use -token or set GITHUB_TOKEN"))
		}
	}

	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: githubToken},
	)
	tc := oauth2.NewClient(ctx, ts)

	client := github.NewClient(tc)
	client.BaseURL = u

	if patchBase == "" || (baseBranch == "" && !noPullRequest) {
		r, _, err := client.Repositories.Get(ctx, owner, repo)
		if err != nil {
			die(1, fmt.Errorf("failed to get repository: %w", err))
		}
		if patchBase == "" {
			patchBase = fmt.Sprintf("refs/heads/%s", r.GetDefaultBranch())
		}
		if baseBranch == "" {
			baseBranch = r.GetDefaultBranch()
		}
	}

	if strings.HasPrefix(patchBase, "refs/") {
		// TODO(bkeyes): go-github uses a secret endpoint for GetRef with
		// additional library-specific error generation
		ref, _, err := client.Git.GetRef(ctx, owner, repo, strings.TrimPrefix(patchBase, "refs/"))
		if err != nil {
			die(1, fmt.Errorf("failed to resolve patch base: %s: %w", patchBase, err))
		}
		patchBase = ref.GetObject().GetSHA()
	}

	commit, _, err := client.Git.GetCommit(ctx, owner, repo, patchBase)
	if err != nil {
		die(1, fmt.Errorf("failed to get patch base commit: %s: %w", patchBase, err))
	}

	root, _, err := client.Git.GetTree(ctx, owner, repo, commit.GetTree().GetSHA(), false)
	if err != nil {
		die(1, fmt.Errorf("failed to get patch base tree: %w", err))
	}

	var patchFile string
	if fs.NArg() == 0 {
		patchFile = "-"
	} else {
		patchFile = fs.Arg(0)
	}

	var r io.ReadCloser
	if patchFile == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(patchFile)
		if err != nil {
			die(1, fmt.Errorf("failed to open patch file: %w", err))
		}
		r = f
	}

	files, preamble, err := gitdiff.Parse(r)
	if err != nil {
		die(1, fmt.Errorf("failed to parse patch: %w", err))
	}
	_ = r.Close()

	var header *gitdiff.PatchHeader
	if len(preamble) > 0 {
		header, _ = gitdiff.ParsePatchHeader(preamble)
		// TODO(bkeyes): warn about ignoring invalid headers
	}

	treeCache := newTreeCache(client, owner, repo, root)

	var entries []*github.TreeEntry
	for _, file := range files {
		// TODO(bkeyes): validate file to make sure fields are consistent
		// maybe two modes: validate and fix, where fix tries to set
		// missing fields based on the framents or the set fields
		switch {
		case file.IsNew:
			var b bytes.Buffer
			enc := base64.NewEncoder(base64.StdEncoding, &b)
			if err := gitdiff.Apply(enc, bytes.NewReader(nil), file); err != nil {
				// TODO(bkeyes): may leave dangling objects
				die(1, fmt.Errorf("failed to apply file: %s: %w", file.NewName, err))
			}
			enc.Close()

			blob, _, err := client.Git.CreateBlob(ctx, owner, repo, &github.Blob{
				Content:  github.String(b.String()),
				Encoding: github.String("base64"),
			})
			if err != nil {
				// TODO(bkeyes): may leave dangling objects
				die(1, fmt.Errorf("failed to create blob for file: %s: %w", file.NewName, err))
			}

			entries = append(entries, &github.TreeEntry{
				Path: github.String(file.NewName),
				// TODO(bkeyes): use default if file.NewMode is unset
				Mode: github.String(strconv.FormatInt(int64(file.NewMode), 8)),
				Type: github.String("blob"),
				SHA:  blob.SHA,
			})

		case file.IsDelete:
			entries = append(entries, &github.TreeEntry{
				Path: github.String(file.OldName),
			})

		default:
			sha, err := treeCache.GetBlobSHA(ctx, file.OldName)
			if err != nil {
				// TODO(bkeyes): may leave dangling objects
				die(1, fmt.Errorf("failed to get blob SHA for file: %s: %w", file.OldName, err))
			}

			if len(file.TextFragments) > 0 || file.BinaryFragment != nil {
				data, _, err := client.Git.GetBlobRaw(ctx, owner, repo, sha)
				if err != nil {
					// TODO(bkeyes): may leave dangling objects
					die(1, fmt.Errorf("failed to get blob content for file: %s: %w", file.OldName, err))
				}

				var b bytes.Buffer
				enc := base64.NewEncoder(base64.StdEncoding, &b)
				if err := gitdiff.Apply(enc, bytes.NewReader(data), file); err != nil {
					// TODO(bkeyes): may leave dangling objects
					die(1, fmt.Errorf("failed to apply file: %s: %w", file.NewName, err))
				}
				enc.Close()

				blob, _, err := client.Git.CreateBlob(ctx, owner, repo, &github.Blob{
					Content:  github.String(b.String()),
					Encoding: github.String("base64"),
				})
				if err != nil {
					// TODO(bkeyes): may leave dangling objects
					die(1, fmt.Errorf("failed to create blob for file: %s: %w", file.NewName, err))
				}
				sha = blob.GetSHA()
			}

			// TODO(bkeyes): use existing mode if file.NewMode is unset
			// see also https://github.com/bluekeyes/go-gitdiff/issues/8
			// need to use default mode if neither is set or GitHub complains
			mode := file.NewMode
			if mode == 0 {
				mode = file.OldMode
			}

			entries = append(entries, &github.TreeEntry{
				Path: github.String(file.NewName),
				Mode: github.String(strconv.FormatInt(int64(mode), 8)),
				Type: github.String("blob"),
				SHA:  github.String(sha),
			})

			// also delete the old file if this is a rename
			if file.OldName != file.NewName {
				entries = append(entries, &github.TreeEntry{
					Path: github.String(file.OldName),
				})
			}
		}
	}

	newTree, _, err := client.Git.CreateTree(ctx, owner, repo, root.GetSHA(), entries)
	if err != nil {
		// TODO(bkeyes): may leave dangling objects
		die(1, fmt.Errorf("failed to create tree for patch: %w", err))
	}

	c := headerToCommit(header, patchFile, message)
	c.Tree = newTree
	c.Parents = []*github.Commit{
		{SHA: github.String(commit.GetSHA())},
	}

	newCommit, _, err := client.Git.CreateCommit(ctx, owner, repo, c)
	if err != nil {
		// TODO(bkeyes): may leave dangling objects
		die(1, fmt.Errorf("failed to create commit for patch: %w", err))
	}

	headRef := fmt.Sprintf("refs/heads/%s", headBranch)

	// TODO(bkeyes): go-github uses a secret endpoint for GetRef
	// https://github.com/google/go-github/issues/1512
	var refExists bool
	if refs, _, err := client.Git.GetRefs(ctx, owner, repo, fmt.Sprintf("heads/%s", headBranch)); err != nil {
		if rerr, ok := err.(*github.ErrorResponse); !ok || rerr.Response.StatusCode != 404 {
			// TODO(bkeyes): may leave dangling objects
			die(1, fmt.Errorf("failed to get reference: %s: %w", headRef, err))
		}
	} else {
		refExists = len(refs) == 1 && refs[0].GetRef() != headRef
	}

	if refExists {
		if _, _, err := client.Git.UpdateRef(ctx, owner, repo, &github.Reference{
			Ref: github.String(headRef),
			Object: &github.GitObject{
				SHA: newCommit.SHA,
			},
		}, force); err != nil {
			// TODO(bkeyes): may leave dangling objects
			die(1, fmt.Errorf("failed to update reference: %s: %w", headRef, err))
		}
	} else {
		if _, _, err := client.Git.CreateRef(ctx, owner, repo, &github.Reference{
			Ref: github.String(headRef),
			Object: &github.GitObject{
				SHA: newCommit.SHA,
			},
		}); err != nil {
			// TODO(bkeyes): may leave dangling objects
			die(1, fmt.Errorf("failed to create reference: %s: %w", headRef, err))
		}
	}

	var pr *github.PullRequest
	if !noPullRequest {
		var title, body string
		if message != "" {
			title, body = splitMessage(message)
		} else {
			title, body = splitMessage(newCommit.GetMessage())
		}

		var err error
		if pr, _, err = client.PullRequests.Create(ctx, owner, repo, &github.NewPullRequest{
			Title: &title,
			Body:  &body,
			Head:  &headBranch,
			Base:  &baseBranch,
		}); err != nil {
			die(1, fmt.Errorf("failed to create pull request: %w", err))
		}
	}

	res := result{
		Commit: newCommit.GetSHA(),
		Tree:   newCommit.GetTree().GetSHA(),
	}
	if pr != nil {
		res.PullRequest = &struct {
			Number int
			URL    string
		}{
			Number: pr.GetNumber(),
			URL:    pr.GetHTMLURL(),
		}
	}

	switch {
	case outputJSON:
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			die(1, fmt.Errorf("failed to encode JSON: %w", err))
		}
	case pr != nil:
		fmt.Println(res.PullRequest.URL)
	default:
		fmt.Println(res.Commit)
	}
}

type result struct {
	Commit      string `json:"commit"`
	Tree        string `json:"tree"`
	PullRequest *struct {
		Number int
		URL    string
	} `json:"pull_request,omitempty"`
}

func splitMessage(m string) (title string, body string) {
	s := bufio.NewScanner(strings.NewReader(m))

	var b strings.Builder
	for s.Scan() {
		line := s.Text()
		if strings.TrimSpace(line) == "" && title == "" {
			title = b.String()
			b.Reset()
		} else {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(line)
		}
	}
	if s.Err() != nil {
		return m, ""
	}

	if title == "" {
		return b.String(), ""
	}
	return title, b.String()
}

func headerToCommit(h *gitdiff.PatchHeader, patchFile string, message string) *github.Commit {
	c := &github.Commit{}

	switch {
	case message != "":
		c.Message = github.String(message)
	case h != nil && (h.Title != "" || h.Body != ""):
		c.Message = github.String(h.Message())
	default:
		if patchFile == "-" {
			c.Message = github.String("Apply patch from stdin")
		} else {
			c.Message = github.String(fmt.Sprintf("Apply %s", patchFile))
		}
	}

	if h == nil {
		c.Author = makeCommitAuthor(nil, nil, "AUTHOR")
		c.Committer = makeCommitAuthor(nil, nil, "COMMITTER")
	} else {
		c.Author = makeCommitAuthor(h.Author, h.AuthorDate, "AUTHOR")
		c.Committer = makeCommitAuthor(h.Committer, h.CommitterDate, "COMMITTER")
	}
	return c
}

func makeCommitAuthor(id *gitdiff.PatchIdentity, d *gitdiff.PatchDate, userType string) *github.CommitAuthor {
	name, email, idOk := idFromEnv(userType)
	date, dateOk := dateFromEnv(userType)
	if id == nil && d == nil && !idOk && !dateOk {
		return nil
	}

	a := &github.CommitAuthor{}
	if idOk {
		a.Name = &name
		a.Email = &email
	} else if id != nil {
		a.Name = &id.Name
		a.Email = &id.Email
	}
	if dateOk {
		a.Date = &date.Parsed
	} else if d != nil && d.IsParsed() {
		a.Date = &d.Parsed
	}
	return a
}

// TODO(bkeyes): what does github do if you give it only one of (name, email) for a commit?
func idFromEnv(idType string) (name, email string, ok bool) {
	name, hasName := os.LookupEnv(fmt.Sprintf("GIT_%s_NAME", idType))
	email, hasEmail := os.LookupEnv(fmt.Sprintf("GIT_%s_EMAIL", idType))
	return name, email, hasName || hasEmail
}

func dateFromEnv(dateType string) (gitdiff.PatchDate, bool) {
	d := gitdiff.ParsePatchDate(os.Getenv(fmt.Sprintf("GIT_%s_DATE", dateType)))
	return d, d.IsParsed()
}

func helpText() string {
	help := `
Usage: patch2pr [options] [patch]

  Create a GitHub pull request from a patch file

  This command parses a patch, applies it, and creates a pull request with the
  result. It does not clone the repository to apply the patch. If no patch file
  is given, the command reads the patch from standard input.

  By default, patch2pr uses the patch header for author and committer
  information, falling back to the authenticated GitHub user if the headers are
  missing or invalid. Callers can override these values using the standard Git
  environment variables:

    GIT_AUTHOR_NAME
    GIT_AUTHOR_EMAIL
    GIT_AUTHOR_DATE
    GIT_COMMITTER_NAME
    GIT_COMMITER_EMAIL
    GIT_COMMITER_DATE

  Override the commit message by using the -message flag.

Options:

  -base-branch=branch  The branch to target with the pull request. If unset,
                       use the repository's default branch.

  -force               Update the head branch even if it exists and is not a
                       fast-forward.

  -head-branch=branch  The branch to create or update with the new commit. If
                       unset, use 'patch2pr'.

  -json                Output information about the new commit and pull request
                       in JSON format.

  -message=message     Message for the commit. Overrides the patch header.

  -no-pull-request     Do not create a pull request after creating a commit.

  -patch-base=base     Base commit to apply the patch to. Can be a SHA1, a
                       branch, or a tag. Branches and tags must start with
                       'refs/heads/' or 'refs/tags/' respectively. If unset,
                       use the repository's default branch.

  -pull-body=body      The body for the pull request. If unset, use the body of
                       the commit message.

  -pull-title=title    The title for the pull request. If unset, use the title
                       of the commit message.

  -repository=repo     Repository to apply the patch to in 'owner/name' format.
                       Required.

  -token=token         GitHub API token with 'repo' scope for authentication.
                       If unset, use the value of the GITHUB_TOKEN environment
                       variable.

  -url=url             GitHub API URL. If unset, use https://api.github.com.

`
	return strings.TrimSpace(help)
}
