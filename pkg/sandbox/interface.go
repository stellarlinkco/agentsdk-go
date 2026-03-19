package sandbox

import (
	"errors"
	"fmt"
)

var (
	// ErrPathDenied indicates the path escapes the configured filesystem allowlist.
	ErrPathDenied = errors.New("sandbox: path denied")
	// ErrSymlinkDetected is returned when validation encounters a symlink hop.
	ErrSymlinkDetected = errors.New("sandbox: symlink detected")
	// ErrDomainDenied indicates outbound traffic targets a host outside the allowlist.
	ErrDomainDenied = errors.New("sandbox: domain denied")
	// ErrResourceExceeded signals a resource budget violation.
	ErrResourceExceeded = errors.New("sandbox: resource limit exceeded")
)

// FileSystemPolicy guards filesystem access.
type FileSystemPolicy interface {
	Allow(path string)
	Validate(path string) error
	Roots() []string
}

// NetworkPolicy guards outbound connections.
type NetworkPolicy interface {
	Allow(domain string)
	Validate(host string) error
	Allowed() []string
}

// ResourceUsage captures measured resource consumption.
type ResourceUsage struct {
	CPUPercent  float64
	MemoryBytes uint64
	DiskBytes   uint64
}

// ResourceLimits constrains runtime consumption.
type ResourceLimits struct {
	MaxCPUPercent  float64
	MaxMemoryBytes uint64
	MaxDiskBytes   uint64
}

// ResourcePolicy enforces resource ceilings.
type ResourcePolicy interface {
	Limits() ResourceLimits
	Validate(usage ResourceUsage) error
}

// ResourceLimiter is a minimal implementation of ResourcePolicy.
type ResourceLimiter struct {
	limits ResourceLimits
}

// NewResourceLimiter builds a limiter with the provided ceilings.
func NewResourceLimiter(limits ResourceLimits) *ResourceLimiter {
	return &ResourceLimiter{limits: limits}
}

// Limits reports the configured ceilings.
func (r *ResourceLimiter) Limits() ResourceLimits {
	if r == nil {
		return ResourceLimits{}
	}
	return r.limits
}

// Validate checks the supplied usage against configured ceilings.
func (r *ResourceLimiter) Validate(usage ResourceUsage) error {
	if r == nil {
		return nil
	}
	limits := r.limits

	if limits.MaxCPUPercent > 0 && usage.CPUPercent > limits.MaxCPUPercent {
		return fmt.Errorf("%w: cpu %.2f > %.2f", ErrResourceExceeded, usage.CPUPercent, limits.MaxCPUPercent)
	}
	if limits.MaxMemoryBytes > 0 && usage.MemoryBytes > limits.MaxMemoryBytes {
		return fmt.Errorf("%w: memory %d > %d", ErrResourceExceeded, usage.MemoryBytes, limits.MaxMemoryBytes)
	}
	if limits.MaxDiskBytes > 0 && usage.DiskBytes > limits.MaxDiskBytes {
		return fmt.Errorf("%w: disk %d > %d", ErrResourceExceeded, usage.DiskBytes, limits.MaxDiskBytes)
	}
	return nil
}

// Manager bundles fs/net/resource policies for callers that only need a single entrypoint.
type Manager struct {
	fs FileSystemPolicy
	nw NetworkPolicy
	rp ResourcePolicy
}

// NewManager wires a sandbox manager using the provided policies.
func NewManager(fs FileSystemPolicy, nw NetworkPolicy, rp ResourcePolicy) *Manager {
	return &Manager{fs: fs, nw: nw, rp: rp}
}

// CheckPath validates filesystem access against the configured policy.
func (m *Manager) CheckPath(path string) error {
	if m == nil || m.fs == nil {
		return nil
	}
	return m.fs.Validate(path)
}

// CheckNetwork validates an outbound hostname.
func (m *Manager) CheckNetwork(host string) error {
	if m == nil || m.nw == nil {
		return nil
	}
	return m.nw.Validate(host)
}

// CheckUsage validates resource consumption.
func (m *Manager) CheckUsage(usage ResourceUsage) error {
	if m == nil || m.rp == nil {
		return nil
	}
	return m.rp.Validate(usage)
}

// Enforce executes every configured guard in order.
func (m *Manager) Enforce(path string, host string, usage ResourceUsage) error {
	if err := m.CheckPath(path); err != nil {
		return err
	}
	if err := m.CheckNetwork(host); err != nil {
		return err
	}
	return m.CheckUsage(usage)
}

// Limits reports the resource ceilings when configured.
func (m *Manager) Limits() ResourceLimits {
	if m == nil || m.rp == nil {
		return ResourceLimits{}
	}
	return m.rp.Limits()
}
