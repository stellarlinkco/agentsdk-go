package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/cexll/agentsdk-go/pkg/api"
	acpproto "github.com/coder/acp-go-sdk"
)

const (
	modeDefaultID acpproto.SessionModeId = "default"
	modeCodeID    acpproto.SessionModeId = "code"

	configPermissionModeID acpproto.SessionConfigId = "permission_mode"

	permissionModeAsk         acpproto.SessionConfigValueId = "ask"
	permissionModeAllowAlways acpproto.SessionConfigValueId = "allow_always"
	permissionModeDenyAlways  acpproto.SessionConfigValueId = "deny_always"
)

type sessionState struct {
	id  acpproto.SessionId
	cwd string

	mu             sync.RWMutex
	rt             *api.Runtime
	modes          acpproto.SessionModeState
	configOptions  []acpproto.SessionConfigOption
	turnCancel     context.CancelFunc
	turnGeneration uint64
}

func newSessionState(id acpproto.SessionId, cwd string) *sessionState {
	return &sessionState{
		id:            id,
		cwd:           cwd,
		modes:         defaultSessionModes(),
		configOptions: defaultSessionConfigOptions(),
	}
}

func (s *sessionState) runtime() *api.Runtime {
	s.mu.RLock()
	rt := s.rt
	s.mu.RUnlock()
	return rt
}

func (s *sessionState) setRuntime(rt *api.Runtime) {
	s.mu.Lock()
	s.rt = rt
	s.mu.Unlock()
}

func (s *sessionState) snapshotModes() *acpproto.SessionModeState {
	s.mu.RLock()
	modes := cloneModeState(s.modes)
	s.mu.RUnlock()
	return &modes
}

func (s *sessionState) snapshotConfigOptions() []acpproto.SessionConfigOption {
	s.mu.RLock()
	options := cloneConfigOptions(s.configOptions)
	s.mu.RUnlock()
	return options
}

func (s *sessionState) hasMode(modeID acpproto.SessionModeId) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, mode := range s.modes.AvailableModes {
		if mode.Id == modeID {
			return true
		}
	}
	return false
}

func (s *sessionState) setMode(modeID acpproto.SessionModeId) {
	s.mu.Lock()
	s.modes.CurrentModeId = modeID
	s.mu.Unlock()
}

func (s *sessionState) setConfigOption(configID acpproto.SessionConfigId, value acpproto.SessionConfigValueId) ([]acpproto.SessionConfigOption, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.configOptions {
		selectConfig := s.configOptions[i].Select
		if selectConfig == nil || selectConfig.Id != configID {
			continue
		}
		if !containsSelectValue(selectConfig.Options, value) {
			return nil, fmt.Errorf("unsupported value %q for config %q", value, configID)
		}
		selectConfig.CurrentValue = value
		s.configOptions[i].Select = selectConfig
		return cloneConfigOptions(s.configOptions), nil
	}

	return nil, fmt.Errorf("unknown config option %q", configID)
}

func (s *sessionState) permissionMode() acpproto.SessionConfigValueId {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, option := range s.configOptions {
		if option.Select == nil {
			continue
		}
		if option.Select.Id == configPermissionModeID {
			return option.Select.CurrentValue
		}
	}
	return permissionModeAsk
}

func (s *sessionState) beginTurn(next context.CancelFunc) (uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.turnCancel != nil {
		return 0, false
	}
	s.turnGeneration++
	generation := s.turnGeneration
	s.turnCancel = next
	return generation, true
}

func (s *sessionState) endTurn(generation uint64) {
	s.mu.Lock()
	if s.turnGeneration == generation {
		s.turnCancel = nil
	}
	s.mu.Unlock()
}

func (s *sessionState) cancelTurn() {
	s.mu.Lock()
	cancel := s.turnCancel
	s.turnCancel = nil
	s.turnGeneration++
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func defaultSessionModes() acpproto.SessionModeState {
	return acpproto.SessionModeState{
		AvailableModes: []acpproto.SessionMode{
			{
				Id:          modeDefaultID,
				Name:        "Default",
				Description: acpproto.Ptr("Balanced general-purpose mode."),
			},
			{
				Id:          modeCodeID,
				Name:        "Code",
				Description: acpproto.Ptr("Coding-focused mode with stronger execution bias."),
			},
		},
		CurrentModeId: modeDefaultID,
	}
}

func defaultSessionConfigOptions() []acpproto.SessionConfigOption {
	values := acpproto.SessionConfigSelectOptionsUngrouped{
		{Name: "Ask", Value: permissionModeAsk},
		{Name: "Allow Always", Value: permissionModeAllowAlways},
		{Name: "Deny Always", Value: permissionModeDenyAlways},
	}

	return []acpproto.SessionConfigOption{
		{
			Select: &acpproto.SessionConfigOptionSelect{
				Type:         "select",
				Id:           configPermissionModeID,
				Name:         "Permission Mode",
				Description:  acpproto.Ptr("Control how tool permission prompts are resolved."),
				CurrentValue: permissionModeAsk,
				Options: acpproto.SessionConfigSelectOptions{
					Ungrouped: &values,
				},
			},
		},
	}
}

func containsSelectValue(options acpproto.SessionConfigSelectOptions, value acpproto.SessionConfigValueId) bool {
	if options.Ungrouped != nil {
		for _, item := range *options.Ungrouped {
			if item.Value == value {
				return true
			}
		}
	}
	if options.Grouped != nil {
		for _, group := range *options.Grouped {
			for _, item := range group.Options {
				if item.Value == value {
					return true
				}
			}
		}
	}
	return false
}

func cloneModeState(state acpproto.SessionModeState) acpproto.SessionModeState {
	var cloned acpproto.SessionModeState
	if err := cloneViaJSON(state, &cloned); err != nil {
		return state
	}
	return cloned
}

func cloneConfigOptions(options []acpproto.SessionConfigOption) []acpproto.SessionConfigOption {
	if len(options) == 0 {
		return nil
	}
	var cloned []acpproto.SessionConfigOption
	if err := cloneViaJSON(options, &cloned); err != nil {
		return append([]acpproto.SessionConfigOption(nil), options...)
	}
	return cloned
}

func cloneViaJSON(src any, dst any) error {
	raw, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}
