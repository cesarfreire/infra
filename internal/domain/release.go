package domain

import "time"

// Release is a published version of this application.
type Release struct {
	Version     string // e.g. v1.4.0
	URL         string // release page
	PublishedAt time.Time
}
