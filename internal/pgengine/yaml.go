package pgengine

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// YamlChain represents a chain with tasks for YAML processing
type YamlChain struct {
	Chain      `yaml:",inline"`
	ClientName string     `db:"client_name" yaml:"client_name,omitempty"`
	Schedule   string     `db:"run_at" yaml:"schedule,omitempty"`
	Live       bool       `db:"live" yaml:"live,omitempty"`
	Tasks      []YamlTask `yaml:"tasks"`
}

// YamlTask extends the basic task structure with Parameters field
type YamlTask struct {
	ChainTask  `yaml:",inline"`
	TaskName   string `db:"task_name" yaml:"name,omitempty"`
	Live       *bool  `yaml:"live,omitempty"`
	Parameters []any  `yaml:"parameters,omitempty"`
}

// YamlConfig represents the root YAML configuration
type YamlConfig struct {
	Chains []YamlChain `yaml:"chains"`
}

// ValidateChain validates a YAML chain configuration
func (c *YamlChain) ValidateChain() error {
	if c.ChainName == "" {
		return fmt.Errorf("chain name is required")
	}

	if c.Schedule == "" {
		return fmt.Errorf("chain schedule is required")
	}

	// Validate cron format
	specialSchedules := []string{"@reboot", "@after", "@every"}
	isSpecial := false
	for _, s := range specialSchedules {
		if strings.HasPrefix(c.Schedule, s) {
			isSpecial = true
			break
		}
	}

	if !isSpecial {
		fields := strings.Fields(c.Schedule)
		if len(fields) != 5 {
			return fmt.Errorf("invalid cron format: %s (expected 5 fields)", c.Schedule)
		}
	}

	if len(c.Tasks) == 0 {
		return fmt.Errorf("chain must have at least one task")
	}

	// Validate each task
	for i, task := range c.Tasks {
		if err := task.ValidateTask(); err != nil {
			return fmt.Errorf("task %d: %w", i+1, err)
		}
	}

	return nil
}

// ValidateTask validates a YAML task configuration
func (t *YamlTask) ValidateTask() error {
	if t.Command == "" {
		return fmt.Errorf("task command is required")
	}

	// Validate kind
	switch strings.ToUpper(t.Kind) {
	case "", "SQL", "PROGRAM", "BUILTIN":
		// Valid kinds
	default:
		return fmt.Errorf("invalid task kind: %s (must be SQL, PROGRAM, or BUILTIN)", t.Kind)
	}

	// Validate timeout is non-negative
	if t.Timeout < 0 {
		return fmt.Errorf("task timeout must be non-negative")
	}

	return nil
}

// SetDefaults sets default values for optional fields
func (c *YamlChain) SetDefaults() {
	// Chain defaults
	if c.Schedule == "" {
		c.Schedule = "* * * * *" // Default to every minute
	}

	// Task defaults
	for i := range c.Tasks {
		task := &c.Tasks[i]
		if task.Kind == "" {
			task.Kind = "SQL"
		}
		// Tasks are live by default, unlike chains
		if task.Live == nil {
			live := true
			task.Live = &live
		}
	}
}

// ParseYamlFile parses a YAML file and returns the configuration
func ParseYamlFile(filePath string) (*YamlConfig, error) {
	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("file not found: %s", filePath)
	}

	// Read file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Parse YAML
	var config YamlConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Set defaults and validate
	for i := range config.Chains {
		chain := &config.Chains[i]
		chain.SetDefaults()
		if err := chain.ValidateChain(); err != nil {
			return nil, fmt.Errorf("chain %d (%s): %w", i+1, chain.ChainName, err)
		}
	}

	return &config, nil
}
