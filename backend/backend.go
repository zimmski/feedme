package backend

import (
	"fmt"

	"github.com/zimmski/feedme"
)

type Backend interface {
	Init(params BackendParameters) error

	CreateItems(feed *feedme.Feed, items []feedme.Item) error

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
	}

	return nil, fmt.Errorf("unknown backend \"%s\"", name)
}
