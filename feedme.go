package feedme

import (
	"time"
)

type Feed struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
	Url  string `json:"url"`
}

type Item struct {
	Feed        int
	Id          int
	Title       string
	Uri         string
	Description string
	Created     time.Time
}
