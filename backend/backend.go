package backend

import (
	"errors"
	"fmt"

	"github.com/zimmski/feedme"
)

type Backend interface {
	Init(params BackendParameters) error

	FindFeed(feedName string) (*feedme.Feed, error)

	SearchFeeds() ([]feedme.Feed, error)
	SearchItems(feed *feedme.Feed) ([]feedme.Item, error)
}

type BackendParameters struct {
	Spec         string
	MaxIdleConns int
	MaxOpenConns int
}

func NewBackend(name string) (Backend, error) {
	if name == "postgresql" {
		return NewBackendPostgresql(), nil
	} else {
		return nil, errors.New(fmt.Sprintf("Unknown backend \"%s\"", name))
	}
}
