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
	modeAskID       acpproto.SessionModeId = "ask"
	modeArchitectID acpproto.SessionModeId = "architect"
	modeCodeID      acpproto.SessionModeId = "code"

	configSessionModeID acpproto.SessionConfigId = "mode"
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
	for i := range s.configOptions {
		selectConfig := s.configOptions[i].Select
		if selectConfig == nil || selectConfig.Id != configSessionModeID {
			continue
		}
		selectConfig.CurrentValue = modeConfigValue(modeID)
		s.configOptions[i].Select = selectConfig
		break
	}
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
		if configID == configSessionModeID {
			modeID := configValueToMode(value)
			if !s.hasModeLocked(modeID) {
				return nil, fmt.Errorf("unsupported mode %q", modeID)
			}
			s.modes.CurrentModeId = modeID
		}
		s.configOptions[i].Select = selectConfig
		return cloneConfigOptions(s.configOptions), nil
	}

	return nil, fmt.Errorf("unknown config option %q", configID)
}

func (s *sessionState) currentMode() acpproto.SessionModeId {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.modes.CurrentModeId
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
				Id:          modeAskID,
				Name:        "Ask",
				Description: acpproto.Ptr("Request permission before making changes."),
			},
			{
				Id:          modeArchitectID,
				Name:        "Architect",
				Description: acpproto.Ptr("Planning-focused mode for design and analysis before implementation."),
			},
			{
				Id:          modeCodeID,
				Name:        "Code",
				Description: acpproto.Ptr("Implementation-focused mode with full coding execution."),
			},
		},
		CurrentModeId: modeAskID,
	}
}

func defaultSessionConfigOptions() []acpproto.SessionConfigOption {
	values := acpproto.SessionConfigSelectOptionsUngrouped{
		{
			Name:        "Ask",
			Value:       modeConfigValue(modeAskID),
			Description: acpproto.Ptr("Request permission before making any changes."),
		},
		{
			Name:        "Architect",
			Value:       modeConfigValue(modeArchitectID),
			Description: acpproto.Ptr("Design and plan software systems without implementation."),
		},
		{
			Name:        "Code",
			Value:       modeConfigValue(modeCodeID),
			Description: acpproto.Ptr("Write and modify code with full tool access."),
		},
	}
	categoryMode := acpproto.SessionConfigOptionCategoryOther("mode")

	return []acpproto.SessionConfigOption{
		{
			Select: &acpproto.SessionConfigOptionSelect{
				Type:        "select",
				Id:          configSessionModeID,
				Name:        "Session Mode",
				Description: acpproto.Ptr("Controls how the agent requests permission and approaches work."),
				Category: &acpproto.SessionConfigOptionCategory{
					Other: &categoryMode,
				},
				CurrentValue: modeConfigValue(modeAskID),
				Options: acpproto.SessionConfigSelectOptions{
					Ungrouped: &values,
				},
			},
		},
	}
}

func modeConfigValue(modeID acpproto.SessionModeId) acpproto.SessionConfigValueId {
	return acpproto.SessionConfigValueId(modeID)
}

func configValueToMode(value acpproto.SessionConfigValueId) acpproto.SessionModeId {
	return acpproto.SessionModeId(value)
}

func (s *sessionState) hasModeLocked(modeID acpproto.SessionModeId) bool {
	for _, mode := range s.modes.AvailableModes {
		if mode.Id == modeID {
			return true
		}
	}
	return false
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
