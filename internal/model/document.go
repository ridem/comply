package model

import "time"

type Document struct {
	Name    string `yaml:"name"`
	Acronym string `yaml:"acronym"`

	Revisions      []Revision   `yaml:"majorRevisions"`
	Satisfies      Satisfaction `yaml:"satisfies"`
	Owner       string       `yaml:"documentOwner"`
	FullPath       string
	OutputFilename string
	ModifiedAt     time.Time
	Body           string
}
