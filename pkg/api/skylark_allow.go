package api

import (
	"sync"
)

// skylarkAllowState tracks which tools are visible/executable in progressive (Skylark) mode.
// Base always includes retrieve_knowledge and retrieve_capabilities; unlocked grows via retrieve_capabilities.
type skylarkAllowState struct {
	mu sync.Mutex

	base             map[string]struct{}
	unlocked         map[string]struct{}
	requestWhitelist map[string]struct{}
}

func newSkylarkAllowState(requestWhitelist map[string]struct{}) *skylarkAllowState {
	s := &skylarkAllowState{
		base: map[string]struct{}{
			"retrieve_knowledge":    {},
			"retrieve_capabilities": {},
		},
		unlocked:         map[string]struct{}{},
		requestWhitelist: requestWhitelist,
	}
	return s
}

func (s *skylarkAllowState) allowedMap() map[string]struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]struct{})
	for k := range s.base {
		out[k] = struct{}{}
	}
	for k := range s.unlocked {
		if len(s.requestWhitelist) > 0 {
			if _, ok := s.requestWhitelist[k]; !ok {
				continue
			}
		}
		out[k] = struct{}{}
	}
	return out
}

func (s *skylarkAllowState) unlock(names []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, raw := range names {
		n := canonicalToolName(raw)
		if n == "" {
			continue
		}
		s.unlocked[n] = struct{}{}
	}
}

// updateWhitelist adds tool names to the requestWhitelist so that
// subsequently unlocked tools are not filtered out by allowedMap().
// Called after a skill execution returns allowed-tools.
func (s *skylarkAllowState) updateWhitelist(tools []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.requestWhitelist == nil {
		s.requestWhitelist = map[string]struct{}{}
	}
	for _, raw := range tools {
		n := canonicalToolName(raw)
		if n == "" {
			continue
		}
		s.requestWhitelist[n] = struct{}{}
	}
}

func (s *skylarkAllowState) isAllowed(name string) bool {
	n := canonicalToolName(name)
	if n == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.base[n]; ok {
		return true
	}
	if _, ok := s.unlocked[n]; !ok {
		return false
	}
	if len(s.requestWhitelist) > 0 {
		if _, ok := s.requestWhitelist[n]; !ok {
			return false
		}
	}
	return true
}
