package writeintent

import (
	"strings"
	"testing"
)

func TestDescriptionPhraseListsCoverPairTools(t *testing.T) {
	if len(CreateDescriptionPhrases(Dashboard)) == 0 {
		t.Fatal("CreateDescriptionPhrases must not be empty")
	}
	if len(UpdateDescriptionPhrases(Dashboard)) == 0 {
		t.Fatal("UpdateDescriptionPhrases must not be empty")
	}

	createJoined := joinPhrases(CreateDescriptionPhrases(Dashboard))
	if !strings.Contains(createJoined, Dashboard.UpdateTool) {
		t.Fatalf("create phrases must mention %s", Dashboard.UpdateTool)
	}
	if !strings.Contains(createJoined, Dashboard.CreateTool) {
		t.Fatalf("create phrases must mention %s", Dashboard.CreateTool)
	}

	updateJoined := joinPhrases(UpdateDescriptionPhrases(Dashboard))
	if !strings.Contains(updateJoined, Dashboard.CreateTool) {
		t.Fatalf("update phrases must mention %s", Dashboard.CreateTool)
	}
}

func joinPhrases(phrases []Phrase) string {
	var b strings.Builder
	for _, p := range phrases {
		b.WriteString(p.Text)
		b.WriteByte(' ')
	}
	return b.String()
}
