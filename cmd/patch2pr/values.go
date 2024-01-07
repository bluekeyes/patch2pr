package main

import (
	"net/url"
	"strings"

	"github.com/bluekeyes/patch2pr"
)

type RepositoryValue struct {
	r **patch2pr.Repository
}

func (v RepositoryValue) String() string {
	if v.r == nil || *v.r == nil {
		return ""
	}
	return (*v.r).String()
}

func (v RepositoryValue) Set(s string) error {
	r, err := patch2pr.ParseRepository(s)
	if err != nil {
		return err
	}
	*v.r = &r
	return nil
}

type URLValue struct {
	u **url.URL
}

func (v URLValue) String() string {
	if v.u == nil || *v.u == nil {
		return ""
	}
	return (*v.u).String()
}

func (v URLValue) Set(s string) error {
	u, err := url.Parse(s)
	if err != nil {
		return err
	}
	if !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	*v.u = u
	return nil
}

type ForkValue struct {
	RepositoryValue
	enabled *bool
}

func (v ForkValue) String() string {
	if v.enabled == nil || !*v.enabled {
		return ""
	}
	return v.RepositoryValue.String()
}

func (v ForkValue) Set(s string) error {
	if err := v.RepositoryValue.Set(s); err != nil {
		return err
	}
	*v.enabled = true
	return nil
}
