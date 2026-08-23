package knowledge

import (
	"strings"
	"testing"
)

func TestChunkerUsesMarkdownHeadingsAsSections(t *testing.T) {
	chunks, err := Chunker{TargetChars: 900, MaxChars: 1200}.Split("faq.md", []byte("# Battery\r\n\r\nLasts 10 hours."))
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1", len(chunks))
	}
	if chunks[0].ChunkIndex != 0 || chunks[0].Section != "Battery" {
		t.Fatalf("chunk = %#v", chunks[0])
	}
	if chunks[0].Content != "Lasts 10 hours." {
		t.Fatalf("content = %q", chunks[0].Content)
	}
	if len(chunks[0].ContentHash) != 64 {
		t.Fatalf("content hash = %q", chunks[0].ContentHash)
	}
}

func TestChunkerNormalizesLineEndingsAndBlankLines(t *testing.T) {
	chunks, err := Chunker{TargetChars: 900, MaxChars: 1200}.Split("spec.txt", []byte("Line 1\r\n\r\n\r\nLine 2\rLine 3"))
	if err != nil {
		t.Fatal(err)
	}
	if got := chunks[0].Content; got != "Line 1\n\nLine 2\nLine 3" {
		t.Fatalf("content = %q", got)
	}
}

func TestChunkerHardSplitsLongParagraph(t *testing.T) {
	chunks, err := Chunker{TargetChars: 900, MaxChars: 1200}.Split("manual.txt", []byte(strings.Repeat("a", 2500)))
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 3 {
		t.Fatalf("len(chunks) = %d, want 3", len(chunks))
	}
	for _, chunk := range chunks {
		if len(chunk.Content) > 1200 {
			t.Fatalf("chunk length = %d, want <= 1200", len(chunk.Content))
		}
	}
}

func TestChunkerRejectsEmptyContent(t *testing.T) {
	_, err := Chunker{TargetChars: 900, MaxChars: 1200}.Split("empty.txt", []byte("\r\n \x00 \n"))
	if err == nil {
		t.Fatal("Split() error = nil, want empty content error")
	}
}

func TestChunkerKeepsStableContentHashes(t *testing.T) {
	chunker := Chunker{TargetChars: 900, MaxChars: 1200}
	first, err := chunker.Split("faq.md", []byte("# Battery\n\nLasts 10 hours."))
	if err != nil {
		t.Fatal(err)
	}
	second, err := chunker.Split("faq.md", []byte("# Battery\r\n\r\nLasts 10 hours."))
	if err != nil {
		t.Fatal(err)
	}
	if first[0].ContentHash != second[0].ContentHash {
		t.Fatalf("hashes differ: %s != %s", first[0].ContentHash, second[0].ContentHash)
	}
}
