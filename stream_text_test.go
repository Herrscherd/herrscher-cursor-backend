package cursor

import (
	"bufio"
	"fmt"
	"slices"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	streamTurnResultLine = `{"type":"result","subtype":"success","is_error":false,"result":"done","session_id":"s"}`
	streamToolCallLine   = `{"type":"tool_call","subtype":"started","tool_call":{"readToolCall":{"args":{"path":"README.md"}}},"session_id":"s"}`
)

func assistantTextLine(text string) string {
	return fmt.Sprintf(`{"type":"assistant","message":{"content":[{"type":"text","text":%q}]},"session_id":"s"}`, text)
}

func readTurnTexts(t *testing.T, lines ...string) ([]string, turnResult) {
	t.Helper()
	input := strings.Join(append(slices.Clone(lines), streamTurnResultLine), "\n") + "\n"
	var texts []string
	turn, err := readTurn(bufio.NewReader(strings.NewReader(input)), func(e contracts.BackendEvent) {
		if e.Kind == "text" {
			texts = append(texts, e.Detail)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	return texts, turn
}

func assertTextDetails(t *testing.T, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("textes émis = %q, attendu %q", got, want)
	}
}

func TestTextBlocksOfATurnAreSeparatedByAParagraphBreak(t *testing.T) {
	texts, turn := readTurnTexts(t, assistantTextLine("Je regarde."), assistantTextLine("Voici le fichier."))
	assertTextDetails(t, texts, []string{"Je regarde.", "\n\nVoici le fichier."})
	if turn.Text != "done" {
		t.Fatalf("turn text = %q, want the result event's text", turn.Text)
	}
}

func TestFirstTextBlockOfATurnHasNoSeparator(t *testing.T) {
	texts, _ := readTurnTexts(t, streamToolCallLine, assistantTextLine("Voici le fichier."))
	assertTextDetails(t, texts, []string{"Voici le fichier."})
}

func TestThinkingAndToolUseBlocksDoNotCountAsEmittedText(t *testing.T) {
	reasoningAndToolUse := `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"Je réfléchis."},{"type":"tool_use","name":"Read","input":{"path":"README.md"}}]},"session_id":"s"}`
	texts, _ := readTurnTexts(t, reasoningAndToolUse, assistantTextLine("Voici le fichier."))
	assertTextDetails(t, texts, []string{"Voici le fichier."})
}

func TestBlankTextBlockDoesNotCountAsEmittedText(t *testing.T) {
	texts, _ := readTurnTexts(t, assistantTextLine(" \n "), assistantTextLine("Je regarde."), assistantTextLine(" \n "), assistantTextLine("Voici."))
	assertTextDetails(t, texts, []string{"Je regarde.", "\n\nVoici."})
}

func TestTextSeparatorDoesNotLeakAcrossTurns(t *testing.T) {
	first, _ := readTurnTexts(t, assistantTextLine("Premier tour."), streamToolCallLine, assistantTextLine("Suite."))
	assertTextDetails(t, first, []string{"Premier tour.", "\n\nSuite."})
	second, _ := readTurnTexts(t, assistantTextLine("Second tour."))
	assertTextDetails(t, second, []string{"Second tour."})
}
