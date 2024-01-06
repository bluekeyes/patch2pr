# patch2pr
[![PkgGoDev](https://pkg.go.dev/badge/github.com/bluekeyes/patch2pr)](https://pkg.go.dev/github.com/bluekeyes/patch2pr)

Create GitHub pull requests from Git patches without cloning the repository.

## Why?

As a command line tool, it's mostly a curiosity and test for the library, but
might have some use for exceptionally large repositories or in environments
where cloning is not feasible.

As a library, however, it enables tools to make automated code changes without
giving every part of system write access or requiring extra logic for managing
clones. One part of the system can generate a patch and send it to another part
that uses this library to apply it and create a pull request.

## Usage: CLI

Install the CLI using `go install`:

    go install github.com/bluekeyes/patch2pr/cmd/patch2pr@latest

The CLI takes a path to a patch file as the only argument or reads a patch from
stdin if no file is given.

The other required arguments are:

- The `-repository` flag to specify the repository in `owner/name` format
- A GitHub token, set with the `-token` flag or in the `GITHUB_TOKEN`
  environment variable.
  - Classic tokens must have `repo` scope
  - Fine-grained tokens must have read and write access to contents and pull
    requests

For example:

    $ export GITHUB_TOKEN="token"
    $ patch2pr -repository bluekeyes/patch2pr /path/to/file.patch

See the CLI help (`-h` or `-help`) or below for full details.

### Full Usage

```
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

  -draft               Create a draft pull request.

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
```

## Usage: Library

The CLI is built on the `patch2pr` library, which can be used to build other
tools that apply patches directly to GitHub. See the [documentation][] for full
details.

The library uses [google/go-github][] to interact with GitHub and exposes types
from that package in the API.

[documentation]: https://pkg.go.dev/github.com/bluekeyes/patch2pr?tab=doc
[google/go-github]: https://github.com/google/go-github

## Stability

Beta. The library is used in a production application that applies thousands of
patches every day, but the interface for both the CLI and the library may
change.

While the underlying patch library ([bluekeyes/go-gitdiff][]) has good test
coverage and real-world usage, the space of all possible patches is large, so
there are likely undiscovered bugs.

[bluekeyes/go-gitdiff]: https://github.com/bluekeyes/go-gitdiff

## Contributing

Contributions are welcome. If reporting an issue with applying a patch, please
include the patch file and the base commit or file content if possible. A link
to a public repository is most helpful.

At this time, I don't intend to support services other than GitHub. If you'd
like support for another service, please file an issue with a link to the
relevant API documentation so I can estimate the work involved in adding the
necessary abstractions.

## License

MIT
