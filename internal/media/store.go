package media

import "context"

type Store interface {
	Put(ctx context.Context, name, contentType string, data []byte) error
	Delete(ctx context.Context, name string) error
	URL(ctx context.Context, name string) (string, error)
}
