package replytext

import (
	"reflect"
	"testing"
)

func TestSplit(t *testing.T) {
	for _, tt := range []struct {
		text string
		want []string
	}{
		{"First\n\n<|split|>\n\nSecond", []string{"First", "Second"}},
		{"你好<|split|>世界", []string{"你好", "世界"}},
		{"<|split|>One<|split|><|split|>", []string{"One"}},
		{"Example `<|split|>`<|split|>Next", []string{"Example `<|split|>`", "Next"}},
		{"```text\n<|split|>\n```<|split|>", []string{"```text\n<|split|>\n```<|split|>"}},
		{"~~~text\n<|split|>\n~~~\n<|split|>Next", []string{"~~~text\n<|split|>\n~~~", "Next"}},
	} {
		if got := Split(tt.text, false); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Split(%q) = %q, want %q", tt.text, got, tt.want)
		}
	}
}

func TestStreamingMarkerPrefixes(t *testing.T) {
	const marker = "<|split|>"
	for n := 1; n <= len(marker); n++ {
		if got := Split("Hello"+marker[:n], true); !reflect.DeepEqual(got, []string{"Hello"}) {
			t.Errorf("prefix %d: %q", n, got)
		}
	}
	if got := Split("Hello<|sample", true); !reflect.DeepEqual(got, []string{"Hello<|sample"}) {
		t.Fatalf("non-marker lost: %q", got)
	}
	if got := Split("Hello<|sp", false); !reflect.DeepEqual(got, []string{"Hello<|sp"}) {
		t.Fatalf("final literal lost: %q", got)
	}
}
