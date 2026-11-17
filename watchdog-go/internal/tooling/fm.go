// SPDX-License-Identifier: Apache-2.0
package tooling

import (
	"regexp"
	"strings"
)

var fmANSI = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

// FMAvailability reads the local system model, independently of PCC availability.
func FMAvailability(output []byte, runErr error) (string, string) {
	msg := strings.TrimSpace(fmANSI.ReplaceAllString(string(output), ""))
	for _, line := range strings.Split(msg, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "system" {
			switch fields[0] {
			case "✓":
				return "available", strings.TrimSpace(line)
			case "✗":
				return "unavailable", strings.TrimSpace(line)
			}
		}
	}
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "license") || strings.Contains(lower, "terms") {
		return "license_required", msg
	}
	if msg == "" && runErr != nil {
		msg = runErr.Error()
	}
	return "unavailable", msg
}
