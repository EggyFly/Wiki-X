package content

import (
	"context"
	"io/fs"
)

type Page struct {
	Key             string
	Route           string
	SourcePath      string
	Title           string
	Description     string
	Body            string
	Draft           bool
	Updated         string
	Tags            []string
	Version         string
	TranslationKey  string
	Protected       bool
	ProtectionLevel string
	EntityKey       string
}

type EntityField struct {
	Name  string
	Label string
	Value string
}

type Entity struct {
	Key    string
	Label  string
	Type   string
	Fields []EntityField
}

type EntitySource interface {
	LoadEntities(ctx context.Context) ([]*Entity, error)
}

type Source interface {
	Load(ctx context.Context) ([]*Page, error)
}

type MediaSource interface {
	MediaFS() fs.FS
}
