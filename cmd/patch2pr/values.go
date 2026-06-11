package main

import (
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
