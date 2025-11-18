package sandbox

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
)

// DomainAllowList guards outbound hosts against a normalized white-list.
type DomainAllowList struct {
	mu    sync.RWMutex
	allow []string
}

// NewDomainAllowList creates an allowlist seeded with hosts.
func NewDomainAllowList(allowed ...string) *DomainAllowList {
	p := &DomainAllowList{}
	for _, host := range allowed {
		p.Allow(host)
	}
	return p
}

// Allow permits traffic towards host (exact or suffix match).
func (p *DomainAllowList) Allow(host string) {
	if p == nil {
		return
	}
	norm := normalizeHost(host)
	if norm == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, existing := range p.allow {
		if existing == norm {
			return
		}
	}
	p.allow = append(p.allow, norm)
}

// Allowed returns the normalised domains kept by the policy.
func (p *DomainAllowList) Allowed() []string {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]string, len(p.allow))
	copy(out, p.allow)
	return out
}

// Validate ensures host belongs to the allowlist.
func (p *DomainAllowList) Validate(host string) error {
	if p == nil {
		return fmt.Errorf("%w: policy not initialised", ErrDomainDenied)
	}
	target := normalizeHost(host)
	if target == "" {
		return fmt.Errorf("%w: empty host", ErrDomainDenied)
	}

	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, allowed := range p.allow {
		if matchesHost(target, allowed) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrDomainDenied, target)
}

func normalizeHost(input string) string {
	host := strings.TrimSpace(strings.ToLower(input))
	if host == "" {
		return ""
	}
	if strings.Contains(host, "://") {
		if u, err := url.Parse(host); err == nil {
			host = u.Host
		}
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	} else if strings.HasPrefix(host, "[") && strings.Contains(host, "]") {
		if u, err := url.Parse("http://" + host); err == nil {
			host = strings.Trim(u.Host, "[]")
		}
	}
	host = strings.Trim(host, "[]")
	host = strings.TrimPrefix(host, ".")
	return host
}

func matchesHost(target, allowed string) bool {
	if allowed == "" {
		return false
	}
	if target == allowed {
		return true
	}
	// wildcard prefix
	if strings.HasPrefix(allowed, "*.") {
		suffix := strings.TrimPrefix(allowed, "*.")
		return strings.HasSuffix(target, suffix)
	}
	return strings.HasSuffix(target, "."+allowed)
}
