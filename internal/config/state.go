package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type Agent struct {
	ServerID        int64     `yaml:"server_id"`
	IP              string    `yaml:"ip"`
	DNSRecordID     string    `yaml:"dns_record_id"`
	URL             string    `yaml:"url"`
	Role            string    `yaml:"role"`
	ServerType      string    `yaml:"server_type"`
	Location        string    `yaml:"location"`
	DeployedAt      time.Time `yaml:"deployed_at"`
	SnapshotVersion string    `yaml:"snapshot_version"`
}

type State struct {
	Agents map[string]*Agent `yaml:"agents"`
}

func NewState() *State {
	return &State{
		Agents: make(map[string]*Agent),
	}
}

func StatePath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "agents.yaml"), nil
}

func LoadState() (*State, error) {
	p, err := StatePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return NewState(), nil
		}
		return nil, fmt.Errorf("could not read state: %w", err)
	}

	state := NewState()
	if err := yaml.Unmarshal(data, state); err != nil {
		return nil, fmt.Errorf("invalid state file: %w", err)
	}

	if state.Agents == nil {
		state.Agents = make(map[string]*Agent)
	}

	return state, nil
}

func (s *State) Save() error {
	dir, err := Dir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("could not create config directory: %w", err)
	}

	data, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("could not marshal state: %w", err)
	}

	p := filepath.Join(dir, "agents.yaml")
	if err := os.WriteFile(p, data, 0600); err != nil {
		return fmt.Errorf("could not write state: %w", err)
	}

	return nil
}

func (s *State) GetAgent(name string) (*Agent, bool) {
	a, ok := s.Agents[name]
	return a, ok
}

func (s *State) SetAgent(name string, agent *Agent) {
	s.Agents[name] = agent
}

func (s *State) RemoveAgent(name string) {
	delete(s.Agents, name)
}
