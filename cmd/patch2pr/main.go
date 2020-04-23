package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
)

func main() {
	var baseBranch, headBranch, headerFile, message, patchBase, pullTitle, repository, title, githubToken, githubURL string
	var force, outputJSON, noPullRequest, squash bool

	fs := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	fs.SetOutput(ioutil.Discard)
	fs.Usage = func() {}

	fs.StringVar(&baseBranch, "base-branch", "", "base-branch")
	fs.BoolVar(&force, "force", false, "force")
	fs.StringVar(&headBranch, "head-branch", "", "head-branch")
	fs.StringVar(&headerFile, "header", "", "header")
	fs.BoolVar(&outputJSON, "json", false, "json")
	fs.StringVar(&message, "message", "", "message")
	fs.BoolVar(&noPullRequest, "no-pull-request", false, "no-pull-request")
	fs.StringVar(&patchBase, "patch-base", "", "patch-base")
	fs.StringVar(&pullTitle, "pull-title", "", "pull-title")
	fs.StringVar(&repository, "repository", "", "repository")
	fs.BoolVar(&squash, "squash", false, "squash")
	fs.StringVar(&title, "title", "", "title")
	fs.StringVar(&githubToken, "token", "", "token")
	fs.StringVar(&githubURL, "url", "https://api.github.com", "url")

	if err := fs.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			fmt.Fprintln(os.Stdout, helpText())
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}

	// if given a ref, resolve to a SHA: https://developer.github.com/v3/git/refs/#get-a-single-reference
	// load default branch if we have no patch base or base branch
	// load the SHA to check existence and get tree: https://developer.github.com/v3/git/commits/#get-a-commit
	// load the tree: https://developer.github.com/v3/git/trees/#get-a-tree

	// for each patch:
	//   parse the patch and header
	//
	//   create empty list of tree objects for creation: https://developer.github.com/v3/git/trees/#create-a-tree
	//   for each file in the path:
	//     if content/mode change:
	//       load the appropriate subtree (cache results): https://developer.github.com/v3/git/trees/#get-a-tree
	//       identify the blob hash from the tree
	//       if content change:
	//         fetch the blob content: https://developer.github.com/v3/git/blobs/#get-a-blob
	//         apply the file to the content
	//         create a new blob: https://developer.github.com/v3/git/blobs/#create-a-blob
	//       add an object to the list with the path, blob, and mode
	//
	//    if addition:
	//      apply file to empty content
	//      create a new blob: https://developer.github.com/v3/git/blobs/#create-a-blob
	//      add an object to the list with the path, blob, and mode
	//
	//    if deletion:
	//      add an object to the list with the path, mode, and null blob
	//
	//   create the tree: https://developer.github.com/v3/git/trees/#create-a-tree
	//   create a commit for the tree: https://developer.github.com/v3/git/commits/#create-a-commit
	//   set commit as new base for next loop
	//
	// create a reference for the last commit: https://developer.github.com/v3/git/refs/#create-a-reference
	// create a pull request for the reference: https://developer.github.com/v3/pulls/#create-a-pull-request
}

func helpText() string {
	help := `
Usage: patch2pr [options] patch [patch...]

  Create a GitHub pull request from a patch file

  This command takes one or more patches, applies them in order, and creates a
  pull request with the resulting commits. It does not clone the repository to
  apply the patches.

  Each positional argument is a path to a patch file. If there are no
  arguments, the command reads the patch from standard input.

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

Options:

  -base-branch=branch  The branch to target with the pull request. If unset,
                       use the repository's default branch.

  -force               Update the head branch even if it exists and is not a
                       fast-forward.

  -head-branch=branch  The branch to create or update with the new commit. If
                       unset, use 'patch2pr'.

  -header=path         Path to an alternate file to parse as the patch header.
                       Implies -squash. Not compatible with -title and -message.

  -json                Output information about the new commit and pull request
                       in JSON format.

  -message=message     Message for the commit. Implies -squash. Not compatible
                       with -header.

  -no-pull-request     Do not create a pull request after creating a commit.

  -patch-base=base     Base commit to apply the patch to. Can be a SHA1, a
                       branch, or a tag. Branches and tags must start with
                       'refs/heads/' or 'refs/tags/' respectively. If unset,
                       use the repository's default branch.

  -pull-body=body      The body for the pull request. If unset, use the commit
                       message.

  -pull-title=title    The title for the pull request. If unset, use the commit
                       title.

  -repository=repo     Repository to apply the patch to in 'owner/name' format.
                       Required.

  -squash              If multiple patches are provided, combine them into a
                       single commit. Has no effect with a single patch.

  -title=title         Title for the commit. Implies -squash. Not compatible
                       with -header.

  -token=token         GitHub API token with 'repo' scope for authentication.
                       If unset, use the value of the GITHUB_TOKEN environment
                       variable.

  -url=url             GitHub API URL. If unset, use https://api.github.com.

`
	return strings.TrimSpace(help)
}
