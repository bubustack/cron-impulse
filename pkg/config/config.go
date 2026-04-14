// Package config defines the configuration schema for the cron impulse.
package config

import "time"

// Config holds the cron impulse configuration.
// The target story is resolved from the Impulse's spec.storyRef, not from config.
type Config struct {
	// Schedules defines the cron schedules to run.
	Schedules []Schedule `json:"schedules" mapstructure:"schedules"`

	// Timezone for parsing cron expressions. Defaults to UTC.
	Timezone string `json:"timezone,omitempty" mapstructure:"timezone"`

	// RunOnStartup triggers all schedules immediately on startup.
	// Useful for testing. Defaults to false.
	RunOnStartup bool `json:"runOnStartup,omitempty" mapstructure:"runOnStartup"`

	// ConcurrencyPolicy controls what happens when a schedule fires
	// while the previous run is still active.
	// "allow" (default): Allow concurrent runs.
	// "forbid": Skip the new run if previous is active.
	// "replace": Cancel the previous run and start a new one.
	ConcurrencyPolicy string `json:"concurrencyPolicy,omitempty" mapstructure:"concurrencyPolicy"`
}

// Schedule defines a single cron schedule.
type Schedule struct {
	// Name is a human-readable identifier for this schedule.
	Name string `json:"name" mapstructure:"name"`

	// Description explains what this schedule does.
	Description string `json:"description,omitempty" mapstructure:"description"`

	// Cron is the cron expression (supports 5-field and 6-field formats).
	// Examples: "0 8 * * *" (8 AM daily), "*/5 * * * *" (every 5 minutes)
	Cron string `json:"cron" mapstructure:"cron"`

	// Inputs are additional inputs passed to the triggered story.
	// These are merged with the Impulse resource's default inputs.
	Inputs map[string]any `json:"inputs,omitempty" mapstructure:"inputs"`

	// Metadata is attached to the StoryRun as annotations.
	Metadata map[string]string `json:"metadata,omitempty" mapstructure:"metadata"`

	// Enabled allows disabling a schedule without removing it.
	// Defaults to true.
	Enabled *bool `json:"enabled,omitempty" mapstructure:"enabled"`

	// Jitter adds random delay (0 to this duration) before triggering.
	// Helps spread load when multiple schedules fire simultaneously.
	Jitter time.Duration `json:"jitter,omitempty" mapstructure:"jitter"`
}

// IsEnabled returns whether this schedule is enabled.
func (s *Schedule) IsEnabled() bool {
	return s.Enabled == nil || *s.Enabled
}
