package store

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

const (
	StatusPending   = "pending"
	StatusPublished = "published"
	StatusRejected  = "rejected"
)

type PageRecord struct {
	ID                     int64
	Route                  string
	Title                  string
	Description            string
	Body                   string
	Tags                   []string
	Draft                  bool
	Protected              bool
	Version                string
	TranslationKey         string
	TranslationBaseVersion string
	UpdatedAt              time.Time
	CurrentRevisionID      *int64
}

type Revision struct {
	ID                     int64
	PageID                 int64
	Route                  string
	Title                  string
	Description            string
	Body                   string
	Tags                   []string
	AuthorID               *int64
	AuthorName             string
	Comment                string
	Status                 string
	BaseRevisionID         *int64
	Version                string
	TranslationKey         string
	TranslationBaseVersion string
	CreatedAt              time.Time
	PublishedAt            *time.Time
}

type RevisionInput struct {
	Route                  string
	Title                  string
	Description            string
	Body                   string
	Tags                   []string
	AuthorID               int64
	Comment                string
	BaseVersion            string
	TranslationKey         string
	TranslationBaseVersion string
	Publish                bool
	AllowProtected         bool
}

type User struct {
	ID              int64
	Name            string
	Email           string
	PasswordHash    string
	Disabled        bool
	TOTPSecret      string
	TOTPEnabled     bool
	TOTPSecretSetAt time.Time
	CreatedAt       time.Time
	Groups          []string
	Rights          map[string]bool
}

func (u *User) HasRight(right string) bool {
	if u == nil {
		return false
	}
	return u.Rights[right]
}

type SessionData struct {
	User      *User
	CSRFToken string
	ExpiresAt time.Time
}

type Media struct {
	ID          int64
	FileName    string
	ContentType string
	Size        int64
	UploaderID  *int64
	CreatedAt   time.Time
}

type Comment struct {
	ID            int64
	Route         string
	UserID        *int64
	UserName      string
	Body          string
	Status        string
	Reports       int
	ReportReasons []string
	CreatedAt     time.Time
}

type CommentReport struct {
	ID             int64
	CommentID      int64
	Route          string
	Body           string
	CommentStatus  string
	ReporterName   string
	Reason         string
	Status         string
	ResolvedAt     *time.Time
	ResolvedByName string
	CreatedAt      time.Time
}

type AuditLog struct {
	ID        int64
	ActorID   *int64
	ActorName string
	Action    string
	Target    string
	Detail    string
	CreatedAt time.Time
}

type Group struct {
	ID        int64
	Name      string
	Rights    []string
	UserCount int
	Builtin   bool
}

type Identity struct {
	ID        int64
	UserID    int64
	Provider  string
	Subject   string
	Username  string
	Email     string
	CreatedAt time.Time
}

func pageVersion(title, description, body string, tags []string, translationKey string) string {
	hash := sha256.New()
	for _, value := range []string{title, description, body, strings.Join(tags, "\x00"), translationKey} {
		hash.Write([]byte(value))
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}
