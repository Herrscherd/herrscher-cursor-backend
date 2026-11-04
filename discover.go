package cursor

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	defaultDiscoverCmd = "cursor-agent"
	authRequiredText   = "authentication required"
	discoverWaitDelay  = 2 * time.Second
)

func discover(ctx context.Context, access contracts.DiscoveryAccess) ([]contracts.ModelSpec, error) {
	command := strings.TrimSpace(access.Getenv("CURSOR_CMD"))
	if command == "" {
		command = defaultDiscoverCmd
	}
	var output bytes.Buffer
	cmd := turnCommand(ctx, []string{command, "models"}, "", &output)
	cmd.Stdout = &output
	cmd.WaitDelay = discoverWaitDelay
	runErr := cmd.Run()
	if isAuthRequired(output.String()) {
		return nil, nil
	}
	if runErr != nil {
		return nil, fmt.Errorf("cursor-agent models: %w", runErr)
	}
	return parseModelsOutput(output.String()), nil
}

func isAuthRequired(output string) bool {
	return strings.Contains(strings.ToLower(output), authRequiredText)
}

func parseModelsOutput(output string) []contracts.ModelSpec {
	var specs []contracts.ModelSpec
	seen := make(map[string]bool)
	for _, line := range strings.Split(stripANSI(output), "\n") {
		spec, ok := parseModelLine(strings.TrimSpace(line))
		if !ok || seen[spec.ID] {
			continue
		}
		seen[spec.ID] = true
		specs = append(specs, spec)
	}
	return specs
}

func parseModelLine(line string) (contracts.ModelSpec, bool) {
	if isListingNoise(line) {
		return contracts.ModelSpec{}, false
	}
	var modelID, label string
	if before, after, found := strings.Cut(line, " - "); found {
		modelID, label = strings.TrimSpace(before), stripMarkers(after)
	} else {
		modelID = stripMarkers(line)
	}
	if modelID == "" || strings.ContainsAny(modelID, " ()") {
		return contracts.ModelSpec{}, false
	}
	return contracts.ModelSpec{ID: modelID, Label: label, Arg: modelID, Route: contracts.RouteNative}, true
}

func isListingNoise(line string) bool {
	return line == "" ||
		strings.HasPrefix(line, "Available models") ||
		strings.HasPrefix(line, "No models available") ||
		strings.HasPrefix(line, "Tip:") ||
		strings.HasPrefix(line, "Error")
}

func stripMarkers(text string) string {
	text = strings.TrimSpace(text)
	if index := strings.LastIndex(text, " ("); index >= 0 && strings.HasSuffix(text, ")") {
		return strings.TrimSpace(text[:index])
	}
	return text
}

func stripANSI(text string) string {
	kept := make([]rune, 0, len(text))
	inEscape := false
	for _, char := range text {
		switch {
		case char == '\x1b':
			inEscape = true
		case inEscape && ((char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z')):
			inEscape = false
		case !inEscape:
			kept = append(kept, char)
		}
	}
	return string(kept)
}
