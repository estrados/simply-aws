package awscli

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

// Run executes an AWS CLI command and returns the raw JSON output.
func Run(args ...string) (json.RawMessage, error) {
	args = append(args, "--output", "json")
	cmd := exec.Command("aws", args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("aws %s: %s", args[0], string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("aws %s: %w", args[0], err)
	}
	return json.RawMessage(out), nil
}
