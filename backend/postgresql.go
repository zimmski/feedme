package backend

import (
	"database/sql"
	"fmt"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"

	"github.com/zimmski/feedme"
)

type Postgresql struct {
	Db *sqlx.DB
}

func NewBackendPostgresql() Backend {
	return new(Postgresql)
}

func (p *Postgresql) Init(params BackendParameters) error {
	var err error

	p.Db, err = sqlx.Connect("postgres", params.Spec)
	if err != nil {
		return fmt.Errorf("cannot connect to database: %v", err)
	}

	err = p.Db.Ping()
	if err != nil {
		return fmt.Errorf("cannot ping database: %v", err)
	}

	p.Db.SetMaxIdleConns(params.MaxIdleConns)
	p.Db.SetMaxOpenConns(params.MaxOpenConns)

	return nil
}

func (p *Postgresql) CreateItems(feed *feedme.Feed, items []feedme.Item) error {
	var err error

	tx, err := p.Db.Begin()
	if err != nil {
		return err
	}

	for _, i := range items {
		_, err = tx.Exec("INSERT INTO items(feed, title, uri, description, created) SELECT $1, $2, $3, $4, CURRENT_TIMESTAMP WHERE NOT EXISTS(SELECT id FROM items WHERE feed = $1 AND title = $2 AND uri = $3 AND description = $4)", feed.ID, i.Title, i.URI, i.Description)
		if err != nil {
			return err
		}
	}

	err = tx.Commit()
	if err != nil {
		return err
	}

	return nil
}

func (p *Postgresql) FindFeed(feedName string) (*feedme.Feed, error) {
	feed := &feedme.Feed{}

	err := p.Db.Get(feed, "SELECT * FROM feeds WHERE name = $1", feedName)
	if err == sql.ErrNoRows {
		return nil, nil
	}

	return feed, err
}

func (p *Postgresql) SearchFeeds() ([]feedme.Feed, error) {
	feeds := []feedme.Feed{}

	err := p.Db.Select(&feeds, "SELECT * FROM feeds ORDER BY name")
	if err == sql.ErrNoRows {
		return nil, nil
	}

	return feeds, err
}

func (p *Postgresql) SearchItems(feed *feedme.Feed) ([]feedme.Item, error) {
	items := []feedme.Item{}

	err := p.Db.Select(&items, "SELECT * FROM items WHERE feed = $1 ORDER BY created LIMIT 10", feed.ID)
	if err == sql.ErrNoRows {
		return nil, nil
	}

	return items, err

}
