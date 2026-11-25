package config

import (
	"encoding/json"
	"fmt"
	"time"
)

// Duration is a time.Duration that is written in JSON as a human readable
// string ("15m", "8h", "90s") instead of a raw number of nanoseconds, which
// would be unreadable in a hand-edited config file.
type Duration time.Duration

// Std returns the value as a standard time.Duration.
func (d Duration) Std() time.Duration {
	return time.Duration(d)
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *Duration) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("duration must be a string such as \"15m\" or \"8h\": %w", err)
	}

	parsed, err := time.ParseDuration(text)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", text, err)
	}

	*d = Duration(parsed)
	return nil
}
