package content

import (
	"context"
	"io/fs"
)

type Page struct {
	Key            string
	Route          string
	SourcePath     string
	Title          string
	Description    string
	Body           string
	Draft          bool
	Updated        string
	Tags           []string
	Version        string
	TranslationKey string
	Protected      bool
}

type Source interface {
	Load(ctx context.Context) ([]*Page, error)
}

type MediaSource interface {
	MediaFS() fs.FS
}
