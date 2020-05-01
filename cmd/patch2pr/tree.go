package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v31/github"
)

type treeCache struct {
	client *github.Client
	owner  string
	repo   string

	root  string
	trees map[string]*github.Tree // sha1 to tree object
}

func newTreeCache(client *github.Client, owner, repo string, root *github.Tree) *treeCache {
	sha := root.GetSHA()
	return &treeCache{
		client: client,
		owner:  owner,
		repo:   repo,
		root:   sha,
		trees: map[string]*github.Tree{
			sha: root,
		},
	}
}

func (c *treeCache) GetBlobSHA(ctx context.Context, path string) (string, error) {
	errNotFound := fmt.Errorf("could not find blob at path: %s", path)

	parts := strings.Split(path, "/")
	dir, name := parts[:len(parts)-1], parts[len(parts)-1]

	tree := c.trees[c.root]
	for _, s := range dir {
		entry, ok := findTreeEntry(tree, s, "tree")
		if !ok {
			return "", errNotFound
		}

		next, ok := c.trees[entry.GetSHA()]
		if !ok {
			var err error
			next, _, err = c.client.Git.GetTree(ctx, c.owner, c.repo, entry.GetSHA(), false)
			if err != nil {
				return "", err
			}
			c.trees[next.GetSHA()] = next
		}
		tree = next
	}

	if entry, ok := findTreeEntry(tree, name, "blob"); ok {
		return entry.GetSHA(), nil
	}
	return "", errNotFound
}

func findTreeEntry(t *github.Tree, name, entryType string) (*github.TreeEntry, bool) {
	for _, entry := range t.Entries {
		if entry.GetPath() == name && entry.GetType() == entryType {
			return entry, true
		}
	}
	return nil, false
}
