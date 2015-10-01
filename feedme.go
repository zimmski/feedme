package feedme

import (
	"time"
)

// Feed represents a feed
type Feed struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	Transform string `json:"transform"`
}

// Item represents an item of a feed
type Item struct {
	Feed        int
	ID          int
	Title       string
	URI         string
	Description string
	Created     time.Time
}
