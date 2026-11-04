package cursor

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	defaultDiscoverCmd = "cursor-agent"
	authRequiredText   = "authentication required"
	discoverWaitDelay  = 2 * time.Second
	discoverOutputMax  = 256 << 10
)

var modelIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

type cappedBuffer struct {
	data     bytes.Buffer
	exceeded bool
}

func (b *cappedBuffer) Write(data []byte) (int, error) {
	accepted := min(len(data), max(discoverOutputMax-b.data.Len(), 0))
	if accepted < len(data) {
		b.exceeded = true
	}
	if _, err := b.data.Write(data[:accepted]); err != nil {
		return 0, err
	}
	return len(data), nil
}

func discover(ctx context.Context, access contracts.DiscoveryAccess) ([]contracts.ModelSpec, error) {
	command := strings.TrimSpace(access.Getenv("CURSOR_CMD"))
	if command == "" {
		command = defaultDiscoverCmd
	}
	var output cappedBuffer
	cmd := turnCommand(ctx, []string{command, "models"}, "", &output)
	cmd.Stdout = &output
	cmd.WaitDelay = discoverWaitDelay
	runErr := cmd.Run()
	if isAuthRequired(output.data.String()) {
		return nil, nil
	}
	if runErr != nil {
		return nil, fmt.Errorf("cursor-agent models: %w", runErr)
	}
	if output.exceeded {
		return nil, &OutputTooLargeError{Limit: discoverOutputMax}
	}
	return parseModelsOutput(output.data.String()), nil
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
	var modelID, label string
	if before, after, found := strings.Cut(line, " - "); found {
		modelID, label = strings.TrimSpace(before), stripMarkers(after)
	} else {
		modelID = stripMarkers(line)
	}
	if !modelIDPattern.MatchString(modelID) {
		return contracts.ModelSpec{}, false
	}
	return contracts.ModelSpec{ID: modelID, Label: label, Arg: modelID, Route: contracts.RouteNative}, true
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
