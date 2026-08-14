package profiles

import "testing"

func TestBuildFlameTreeMergesCommonRoot(t *testing.T) {
	rows := []FlamegraphRow{
		{StackHash: "1", Frames: "leafA" + FrameDelimiter + "main", Samples: 10},
		{StackHash: "2", Frames: "leafB" + FrameDelimiter + "main", Samples: 5},
	}

	tree := BuildFlameTree(rows)
	if tree.Value != 15 {
		t.Fatalf("root value=%v want 15", tree.Value)
	}
	if len(tree.Children) != 1 {
		t.Fatalf("children=%d want 1", len(tree.Children))
	}
	mainNode := tree.Children[0]
	if mainNode.Name != "main" || mainNode.Value != 15 {
		t.Fatalf("main node=%+v", mainNode)
	}
	if len(mainNode.Children) != 2 {
		t.Fatalf("main children=%d want 2", len(mainNode.Children))
	}
}

func TestBuildFlameTreeSingleFrameSelf(t *testing.T) {
	tree := BuildFlameTree([]FlamegraphRow{{StackHash: "1", Frames: "onlyFrame", Samples: 42}})
	if len(tree.Children) != 1 {
		t.Fatalf("children=%d", len(tree.Children))
	}
	node := tree.Children[0]
	if node.Self != 42 || node.Value != 42 {
		t.Fatalf("node=%+v", node)
	}
}

func TestFoldToTopFunctionsSelfAndTotal(t *testing.T) {
	rows := []FlamegraphRow{
		{StackHash: "1", Frames: "frameA" + FrameDelimiter + "frameB", Samples: 10},
		{StackHash: "2", Frames: "frameB" + FrameDelimiter + "frameA", Samples: 5},
	}
	result := FoldToTopFunctions(rows)
	byName := map[string]TopFunction{}
	for _, fn := range result {
		byName[fn.Name] = fn
	}
	if byName["frameA"].SelfSamples != 10 || byName["frameA"].TotalSamples != 15 {
		t.Fatalf("frameA=%+v", byName["frameA"])
	}
	if byName["frameB"].SelfSamples != 5 || byName["frameB"].TotalSamples != 15 {
		t.Fatalf("frameB=%+v", byName["frameB"])
	}
}

func TestFoldToTopFunctionsNoDoubleCountRecursive(t *testing.T) {
	rows := []FlamegraphRow{
		{StackHash: "1", Frames: "recurse" + FrameDelimiter + "recurse" + FrameDelimiter + "main", Samples: 7},
	}
	result := FoldToTopFunctions(rows)
	var recurse TopFunction
	for _, fn := range result {
		if fn.Name == "recurse" {
			recurse = fn
		}
	}
	if recurse.TotalSamples != 7 || recurse.SelfSamples != 7 {
		t.Fatalf("recurse=%+v", recurse)
	}
}

func TestBuildProfileSummaryText(t *testing.T) {
	summary := buildProfileSummaryText("api", "cpu", []TopFunction{
		{Name: "hot.A", SelfSamples: 40},
		{Name: "hot.B", SelfSamples: 30},
		{Name: "hot.C", SelfSamples: 10},
	}, 100)
	want := "Top 3 CPU consumers for api are hot.A, hot.B, hot.C, accounting for 80.0% of total self samples."
	if summary != want {
		t.Fatalf("summary=%q want %q", summary, want)
	}
}
