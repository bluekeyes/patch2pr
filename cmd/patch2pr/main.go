package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"github.com/bluekeyes/patch2pr"
	"github.com/google/go-github/v57/github"
	"golang.org/x/oauth2"
)

func die(code int, err error) {
	fmt.Fprintln(os.Stderr, "error:", err)

	if isNotFound(err) {
		fmt.Fprint(os.Stderr, `
This may be because the repository does not exit or the token you are using
does not have write permission. If submitting a patch to a repository where you
do not have write access, consider using the -fork flag to submit the patch
from a fork.
`,
		)
	}

	os.Exit(code)
}

type Options struct {
	BaseBranch     string
	Draft          bool
	Force          bool
	Fork           bool
	ForkRepository *patch2pr.Repository
	HeadBranch     string
	OutputJSON     bool
	Message        string
	NoPullRequest  bool
	PatchBase      string
	PullTitle      string
	Repository     *patch2pr.Repository
	GitHubToken    string
	GitHubURL      *url.URL
	PullBody       string
}

func main() {
	opts := Options{
		GitHubURL: &url.URL{Scheme: "https", Host: "api.github.com", Path: "/"},
	}

	fs := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	fs.SetOutput(ioutil.Discard)
	fs.Usage = func() {}

	fs.StringVar(&opts.BaseBranch, "base-branch", "", "base-branch")
	fs.BoolVar(&opts.Draft, "draft", false, "draft")
	fs.BoolVar(&opts.Force, "force", false, "force")
	fs.BoolVar(&opts.Fork, "fork", false, "fork")
	fs.Var(ForkValue{RepositoryValue{&opts.ForkRepository}, &opts.Fork}, "fork-repository", "fork-repository")
	fs.StringVar(&opts.HeadBranch, "head-branch", "patch2pr", "head-branch")
	fs.BoolVar(&opts.OutputJSON, "json", false, "json")
	fs.StringVar(&opts.Message, "message", "", "message")
	fs.BoolVar(&opts.NoPullRequest, "no-pull-request", false, "no-pull-request")
	fs.StringVar(&opts.PatchBase, "patch-base", "", "patch-base")
	fs.StringVar(&opts.PullBody, "pull-body", "", "pull-body")
	fs.StringVar(&opts.PullTitle, "pull-title", "", "pull-title")
	fs.Var(RepositoryValue{&opts.Repository}, "repository", "repository")
	fs.StringVar(&opts.GitHubToken, "token", "", "token")
	fs.Var(URLValue{&opts.GitHubURL}, "url", "url")

	if err := fs.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			fmt.Fprintln(os.Stdout, helpText())
			os.Exit(0)
		}
		die(2, err)
	}

	fmt.Println("fork:", opts.Fork)
	fmt.Println("fork-repository:", opts.ForkRepository)

	if opts.Repository == nil {
		die(2, errors.New("the -repository flag is required"))
	}
	if opts.GitHubToken == "" {
		if t, ok := os.LookupEnv("GITHUB_TOKEN"); ok {
			opts.GitHubToken = t
		} else {
			die(2, errors.New("a github token is required; use -token or set GITHUB_TOKEN"))
		}
	}

	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: opts.GitHubToken},
	)
	tc := oauth2.NewClient(ctx, ts)

	client := github.NewClient(tc)
	client.BaseURL = opts.GitHubURL

	var patchFile string
	if fs.NArg() == 0 {
		patchFile = "-"
	} else {
		patchFile = fs.Arg(0)
	}

	res, err := execute(ctx, client, patchFile, &opts)
	if err != nil {
		die(1, err)
	}

	switch {
	case opts.OutputJSON:
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			die(1, fmt.Errorf("json encoding failed: %w", err))
		}
	case res.PullRequest != nil:
		fmt.Println(res.PullRequest.URL)
	default:
		fmt.Println(res.Commit)
	}
}

type Result struct {
	Commit      string             `json:"commit"`
	Tree        string             `json:"tree"`
	PullRequest *PullRequestResult `json:"pull_request,omitempty"`
}

type PullRequestResult struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
}

func execute(ctx context.Context, client *github.Client, patchFile string, opts *Options) (*Result, error) {
	targetRepo := *opts.Repository
	patchBase, baseBranch, headBranch := opts.PatchBase, opts.BaseBranch, opts.HeadBranch

	if patchBase == "" || (baseBranch == "" && !opts.NoPullRequest) {
		r, _, err := client.Repositories.Get(ctx, targetRepo.Owner, targetRepo.Name)
		if err != nil {
			return nil, fmt.Errorf("get repository failed: %w", err)
		}
		if patchBase == "" {
			patchBase = fmt.Sprintf("refs/heads/%s", r.GetDefaultBranch())
		}
		if baseBranch == "" {
			baseBranch = r.GetDefaultBranch()
		}
	}

	if strings.HasPrefix(patchBase, "refs/") {
		ref, _, err := client.Git.GetRef(ctx, targetRepo.Owner, targetRepo.Name, strings.TrimPrefix(patchBase, "refs/"))
		if err != nil {
			return nil, fmt.Errorf("get ref for patch base %q failed: %w", patchBase, err)
		}
		patchBase = ref.GetObject().GetSHA()
	}

	commit, _, err := client.Git.GetCommit(ctx, targetRepo.Owner, targetRepo.Name, patchBase)
	if err != nil {
		return nil, fmt.Errorf("get commit for %s failed: %w", patchBase, err)
	}

	var r io.ReadCloser
	if patchFile == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(patchFile)
		if err != nil {
			return nil, fmt.Errorf("open patch file failed: %w", err)
		}
		r = f
	}

	files, preamble, err := gitdiff.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("parsing patch failed: %w", err)
	}
	_ = r.Close()

	var header *gitdiff.PatchHeader
	if len(preamble) > 0 {
		header, err = gitdiff.ParsePatchHeader(preamble)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: invalid patch header: %v", err)
		}
	}

	sourceRepo, err := prepareSourceRepo(ctx, client, opts)
	if err != nil {
		return nil, err
	}

	applier := patch2pr.NewApplier(client, sourceRepo, commit)
	for _, file := range files {
		if _, err := applier.Apply(ctx, file); err != nil {
			name := file.NewName
			if name == "" {
				name = file.OldName
			}
			return nil, fmt.Errorf("apply failed: %s: %w", name, err)
		}
	}

	newCommit, err := applier.Commit(ctx, nil, fillHeader(header, patchFile, opts.Message))
	if err != nil {
		return nil, fmt.Errorf("commit failed: %w", err)
	}

	ref := patch2pr.NewReference(client, sourceRepo, fmt.Sprintf("refs/heads/%s", headBranch))
	if err := ref.Set(ctx, newCommit.GetSHA(), opts.Force); err != nil {
		return nil, fmt.Errorf("set ref failed: %w", err)
	}

	var pr *github.PullRequest
	if !opts.NoPullRequest {
		var title, body string
		if opts.Message != "" {
			title, body = splitMessage(opts.Message)
		} else {
			title, body = splitMessage(newCommit.GetMessage())
		}

		if opts.PullTitle != "" {
			title = opts.PullTitle
		}

		if opts.PullBody != "" {
			body = opts.PullBody
		}

		prSpec := &github.NewPullRequest{
			Title: &title,
			Body:  &body,
			Base:  &baseBranch,
			Draft: &opts.Draft,
		}

		if sourceRepo == targetRepo {
			prSpec.Head = &headBranch
		} else {
			prSpec.Head = github.String(fmt.Sprintf("%s:%s", sourceRepo.Owner, headBranch))
			prSpec.HeadRepo = &sourceRepo.Name
		}

		if pr, _, err = client.PullRequests.Create(ctx, targetRepo.Owner, targetRepo.Name, prSpec); err != nil {
			return nil, fmt.Errorf("create pull request failed: %w", err)
		}
	}

	res := &Result{
		Commit: newCommit.GetSHA(),
		Tree:   newCommit.GetTree().GetSHA(),
	}
	if pr != nil {
		res.PullRequest = &PullRequestResult{
			Number: pr.GetNumber(),
			URL:    pr.GetHTMLURL(),
		}
	}
	return res, nil
}

func prepareSourceRepo(ctx context.Context, client *github.Client, opts *Options) (patch2pr.Repository, error) {
	source := patch2pr.Repository{}
	target := *opts.Repository

	if !opts.Fork {
		// If we're not using a fork, the source is the same as the target
		return target, nil
	}

	user, _, err := client.Users.Get(ctx, "")
	if err != nil {
		return source, fmt.Errorf("get user failed: %w", err)
	}

	if opts.ForkRepository != nil {
		source = *opts.ForkRepository
	} else {
		source = patch2pr.Repository{
			Owner: user.GetLogin(),
			Name:  target.Name,
		}
	}

	repo, _, err := client.Repositories.Get(ctx, source.Owner, source.Name)
	switch {
	case isNotFound(err):
		isUserFork := user.GetLogin() == source.Owner
		if err := createFork(ctx, client, source, target, isUserFork); err != nil {
			return source, fmt.Errorf("forking repository failed: %w", err)
		}

	case err != nil:
		return source, fmt.Errorf("get fork repository failed: %w", err)

	default:
		if !repo.GetFork() || repo.GetParent().GetFullName() != target.String() {
			return source, fmt.Errorf("fork repository %q exists, but is not a fork of %q", source, target)
		}
	}

	return source, nil
}

func createFork(ctx context.Context, client *github.Client, fork, parent patch2pr.Repository, isUserFork bool) error {
	const pollInterval = 5 * time.Second
	const maxWait = 1 * time.Minute

	organization := fork.Owner
	if isUserFork {
		organization = ""
	}

	repo, _, err := client.Repositories.CreateFork(ctx, parent.Owner, parent.Name, &github.RepositoryCreateForkOptions{
		Organization:      organization,
		Name:              fork.Name,
		DefaultBranchOnly: true,
	})

	var aerr *github.AcceptedError
	if err != nil && !errors.As(err, &aerr) {
		return err
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	ctx, cancel := context.WithTimeout(ctx, maxWait)
	defer cancel()

	// Poll the new repo until the default branch exists, indicating it is ready to use
	ref := "heads/" + repo.GetDefaultBranch()
	for {
		select {
		case <-ticker.C:
			if _, _, err := client.Git.GetRef(ctx, fork.Owner, fork.Name, ref); err == nil {
				return nil
			} else if !isNotFound(err) && !errors.Is(err, context.DeadlineExceeded) {
				fmt.Fprintf(os.Stderr, "warning: error while waiting for fork to be ready, will try again: %v", err)
			}

		case <-ctx.Done():
			return fmt.Errorf("fork repository was not ready after %s", maxWait)
		}
	}
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

func fillHeader(h *gitdiff.PatchHeader, patchFile, message string) *gitdiff.PatchHeader {
	if h == nil {
		h = &gitdiff.PatchHeader{}
	}

	if message != "" {
		h.Title, h.Body = splitMessage(message)
	}
	if h.Title == "" && h.Body == "" {
		if patchFile == "-" {
			h.Title = "Apply patch from stdin"
		} else {
			h.Title = fmt.Sprintf("Apply %s", patchFile)
		}
	}

	if envAuthor := idFromEnv("AUTHOR"); envAuthor != nil {
		h.Author = envAuthor
	}
	if envAuthorDate := dateFromEnv("AUHTOR"); !envAuthorDate.IsZero() {
		h.AuthorDate = envAuthorDate
	}
	if envCommitter := idFromEnv("COMMITTER"); envCommitter != nil {
		h.Committer = envCommitter
	}
	if envCommitterDate := dateFromEnv("COMMITTER"); !envCommitterDate.IsZero() {
		h.CommitterDate = envCommitterDate
	}

	return h
}

func idFromEnv(idType string) *gitdiff.PatchIdentity {
	name, hasName := os.LookupEnv(fmt.Sprintf("GIT_%s_NAME", idType))
	email, hasEmail := os.LookupEnv(fmt.Sprintf("GIT_%s_EMAIL", idType))
	if hasName && hasEmail {
		return &gitdiff.PatchIdentity{Name: name, Email: email}
	}
	return nil
}

func dateFromEnv(dateType string) time.Time {
	if d, err := gitdiff.ParsePatchDate(os.Getenv(fmt.Sprintf("GIT_%s_DATE", dateType))); err == nil {
		return d
	}
	return time.Time{}
}

func isNotFound(err error) bool {
	var rerr *github.ErrorResponse
	return errors.As(err, &rerr) && rerr.Response.StatusCode == http.StatusNotFound
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

  -base-branch=branch    The branch to target with the pull request. If unset,
                         use the repository's default branch.

  -draft                 Create a draft pull request.

  -force                 Update the head branch even if it exists and is not a
                         fast-forward.

  -fork                  Submit the pull request from a fork instead of pushing
                         directly to the repository. With no other flags, use a
                         fork in the current account with the same name as the
                         target repository, creating the fork if it does not exist.

  -fork-repository=repo  Submit the pull request from the named fork instead of
                         pushing directly to the repository, creating the fork
                         if it does not exist. Implies the -fork flag.

  -head-branch=branch    The branch to create or update with the new commit. If
                         unset, use 'patch2pr'.

  -json                  Output information about the new commit and pull request
                         in JSON format.

  -message=message       Message for the commit. Overrides the patch header.

  -no-pull-request       Do not create a pull request after creating a commit.

  -patch-base=base       Base commit to apply the patch to. Can be a SHA1, a
                         branch, or a tag. Branches and tags must start with
                         'refs/heads/' or 'refs/tags/' respectively. If unset,
                         use the repository's default branch.

  -pull-body=body        The body for the pull request. If unset, use the body of
                         the commit message.

  -pull-title=title      The title for the pull request. If unset, use the title
                         of the commit message.

  -repository=repo       Repository to apply the patch to in 'owner/name' format.
                         Required.

  -token=token           GitHub API token with 'repo' scope for authentication.
                         If unset, use the value of the GITHUB_TOKEN environment
                         variable.

  -url=url               GitHub API URL. If unset, use https://api.github.com.

`
	return strings.TrimSpace(help)
}
