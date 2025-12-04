package awscli

import (
	"encoding/json"
	"os/exec"
	"strings"
)

type Status struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Region    string `json:"region,omitempty"`
	AccountID string `json:"accountId,omitempty"`
	Profile   string `json:"profile,omitempty"`
}

func Detect() Status {
	s := Status{}

	// Check if aws CLI exists
	out, err := exec.Command("aws", "--version").CombinedOutput()
	if err != nil {
		return s
	}
	s.Installed = true
	s.Version = strings.TrimSpace(strings.Split(string(out), " ")[0])

	// Get configured region
	regionOut, err := exec.Command("aws", "configure", "get", "region").Output()
	if err == nil {
		s.Region = strings.TrimSpace(string(regionOut))
	}

	// Get configured profile
	profileOut, err := exec.Command("aws", "configure", "list").Output()
	if err == nil {
		for _, line := range strings.Split(string(profileOut), "\n") {
			if strings.Contains(line, "profile") {
				fields := strings.Fields(line)
				if len(fields) >= 2 && fields[1] != "<not" {
					s.Profile = fields[1]
				}
			}
		}
	}

	// Get account ID
	identityOut, err := exec.Command("aws", "sts", "get-caller-identity", "--output", "json").Output()
	if err == nil {
		var identity struct {
			Account string `json:"Account"`
		}
		if json.Unmarshal(identityOut, &identity) == nil {
			s.AccountID = identity.Account
		}
	}

	return s
}
