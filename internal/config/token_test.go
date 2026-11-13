package config

import "testing"

func TestHashIsNotACommentInsideWord(t *testing.T) {
	nodes, err := Parse("stream a {\n  transcoder vb=700k size=960x-1:fit:#000000;\n}\n# trailing comment\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(nodes))
	}
	got := nodes[0].Children[0].Arg(0)
	if got != "vb=700k" {
		t.Fatalf("first arg = %q", got)
	}
	if len(nodes[0].Children[0].Args) != 2 {
		t.Fatalf("args = %v", nodes[0].Children[0].Args)
	}
}
