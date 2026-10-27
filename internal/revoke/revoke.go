package revoke

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

const (
	SectionPage        = "page"
	SectionUser        = "user"
	SectionGroup       = "group"
	SectionEntity      = "entity"
	SectionInvite      = "invite"
	SectionOIDCClient  = "oidc_client"
	SectionWatchlist   = "watchlist"
	SectionInteractive = "interactive"
)

var sectionSet = map[string]bool{
	SectionPage:        true,
	SectionUser:        true,
	SectionGroup:       true,
	SectionEntity:      true,
	SectionInvite:      true,
	SectionOIDCClient:  true,
	SectionWatchlist:   true,
	SectionInteractive: true,
}

type Policy struct {
	Sections map[string]bool `json:"sections"`
}

func (p Policy) Enabled(section string) bool {
	if p.Sections == nil {
		return true
	}
	enabled, ok := p.Sections[section]
	if !ok {
		return true
	}
	return enabled
}

func Sections() []string {
	out := make([]string, 0, len(sectionSet))
	for section := range sectionSet {
		out = append(out, section)
	}
	sort.Strings(out)
	return out
}

func Load(path string) (Policy, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Policy{}, nil
	}
	if err != nil {
		return Policy{}, err
	}
	var policy Policy
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return Policy{}, fmt.Errorf("parse %s: %w", path, err)
	}
	for section := range policy.Sections {
		if !sectionSet[section] {
			return Policy{}, fmt.Errorf("unknown revoke section %q", section)
		}
	}
	return policy, nil
}
