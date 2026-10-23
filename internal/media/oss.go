package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	ossdk "github.com/aliyun/aliyun-oss-go-sdk/oss"
)

type OSSOptions struct {
	Endpoint        string
	Bucket          string
	AccessKeyID     string
	AccessKeySecret string
	PublicBaseURL   string
	PathPrefix      string
	ACL             string
	SignedURLTTL    time.Duration
}

type OSS struct {
	bucket  *ossdk.Bucket
	baseURL string
	prefix  string
	acl     ossdk.ACLType
	ttl     time.Duration
}

func NewOSS(opts OSSOptions) (*OSS, error) {
	if opts.Endpoint == "" || opts.Bucket == "" || opts.AccessKeyID == "" || opts.AccessKeySecret == "" {
		return nil, errors.New("oss endpoint, bucket and credentials are required")
	}
	acl, err := ossACL(opts.ACL)
	if err != nil {
		return nil, err
	}
	if opts.SignedURLTTL <= 0 {
		return nil, errors.New("oss signed url ttl must be positive")
	}
	client, err := ossdk.New(opts.Endpoint, opts.AccessKeyID, opts.AccessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("create oss client: %w", err)
	}
	bucket, err := client.Bucket(opts.Bucket)
	if err != nil {
		return nil, fmt.Errorf("open oss bucket: %w", err)
	}
	baseURL := strings.TrimRight(strings.TrimSpace(opts.PublicBaseURL), "/")
	if baseURL == "" {
		baseURL, err = defaultPublicBase(opts.Endpoint, opts.Bucket)
		if err != nil {
			return nil, err
		}
	}
	return &OSS{
		bucket:  bucket,
		baseURL: baseURL,
		prefix:  strings.Trim(strings.TrimSpace(opts.PathPrefix), "/"),
		acl:     acl,
		ttl:     opts.SignedURLTTL,
	}, nil
}

func (s *OSS) Put(ctx context.Context, name, contentType string, data []byte) error {
	if err := safeName(name); err != nil {
		return err
	}
	return s.bucket.PutObject(s.key(name), bytes.NewReader(data), ossdk.ContentType(contentType), ossdk.ObjectACL(s.acl))
}

func (s *OSS) Delete(ctx context.Context, name string) error {
	if err := safeName(name); err != nil {
		return err
	}
	err := s.bucket.DeleteObject(s.key(name))
	var serviceErr ossdk.ServiceError
	if errors.As(err, &serviceErr) && serviceErr.StatusCode == http.StatusNotFound {
		return nil
	}
	return err
}

func (s *OSS) URL(ctx context.Context, name string) (string, error) {
	if err := safeName(name); err != nil {
		return "", err
	}
	key := s.key(name)
	if s.acl == ossdk.ACLPrivate {
		return s.bucket.SignURL(key, ossdk.HTTPGet, int64(s.ttl/time.Second))
	}
	return s.baseURL + "/" + escapeKey(key), nil
}

func (s *OSS) key(name string) string {
	if s.prefix == "" {
		return name
	}
	return s.prefix + "/" + name
}

func ossACL(value string) (ossdk.ACLType, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "private":
		return ossdk.ACLPrivate, nil
	case "public-read":
		return ossdk.ACLPublicRead, nil
	case "public-read-write":
		return ossdk.ACLPublicReadWrite, nil
	default:
		return "", fmt.Errorf("unsupported oss acl %q", value)
	}
}

func defaultPublicBase(endpoint, bucket string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("oss endpoint must be an absolute url")
	}
	host := parsed.Host
	if !strings.HasPrefix(host, bucket+".") {
		host = bucket + "." + host
	}
	return parsed.Scheme + "://" + host, nil
}

func escapeKey(key string) string {
	parts := strings.Split(key, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}
