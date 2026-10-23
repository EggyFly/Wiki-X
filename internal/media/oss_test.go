package media

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestOSSDefaults(t *testing.T) {
	base, err := defaultPublicBase("https://oss-cn-hangzhou.aliyuncs.com", "my-bucket")
	if err != nil || base != "https://my-bucket.oss-cn-hangzhou.aliyuncs.com" {
		t.Fatalf("base = %q, %v", base, err)
	}
	base, err = defaultPublicBase("https://my-bucket.oss-cn-hangzhou.aliyuncs.com", "my-bucket")
	if err != nil || base != "https://my-bucket.oss-cn-hangzhou.aliyuncs.com" {
		t.Fatalf("base = %q, %v", base, err)
	}
	if got := escapeKey("media/a b.mp3"); got != "media/a%20b.mp3" {
		t.Fatalf("escaped = %q", got)
	}
}

func TestOSSACL(t *testing.T) {
	for _, value := range []string{"private", "public-read", "public-read-write"} {
		if _, err := ossACL(value); err != nil {
			t.Fatalf("%s: %v", value, err)
		}
	}
	if _, err := ossACL("public"); err == nil {
		t.Fatal("invalid acl should fail")
	}
}

func TestOSSPublicURL(t *testing.T) {
	store, err := NewOSS(OSSOptions{
		Endpoint:        "https://oss-cn-hangzhou.aliyuncs.com",
		Bucket:          "my-bucket",
		AccessKeyID:     "id",
		AccessKeySecret: "secret",
		PublicBaseURL:   "https://cdn.example.com/base",
		PathPrefix:      "media",
		ACL:             "public-read",
		SignedURLTTL:    10 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := store.URL(context.Background(), "a.png")
	if err != nil || u != "https://cdn.example.com/base/media/a.png" {
		t.Fatalf("url = %q, %v", u, err)
	}
}

func TestOSSPrivateURL(t *testing.T) {
	store, err := NewOSS(OSSOptions{
		Endpoint:        "https://oss-cn-hangzhou.aliyuncs.com",
		Bucket:          "my-bucket",
		AccessKeyID:     "id",
		AccessKeySecret: "secret",
		PathPrefix:      "media",
		ACL:             "private",
		SignedURLTTL:    10 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := store.URL(context.Background(), "a.png")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(u, "Signature=") || (!strings.Contains(u, "/media/a.png") && !strings.Contains(u, "/media%2Fa.png")) {
		t.Fatalf("signed url = %q", u)
	}
}
