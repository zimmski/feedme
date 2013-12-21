package feedme

import (
	"time"
)

type Feed struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	Transform string `json:"transform"`
}

type Item struct {
	Feed        int
	ID          int
	Title       string
	URI         string
	Description string
	Created     time.Time
}
