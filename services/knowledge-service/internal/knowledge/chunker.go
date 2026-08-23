package knowledge

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"
)

var blankLinesPattern = regexp.MustCompile(`\n{3,}`)

type Chunker struct {
	TargetChars int
	MaxChars    int
}

func (c Chunker) Split(fileName string, content []byte) ([]ChunkDraft, error) {
	target, max := c.limits()
	text := NormalizeText(string(content))
	if text == "" {
		return nil, errors.New("empty content")
	}

	sections := splitSections(fileName, text)
	chunks := make([]ChunkDraft, 0, len(sections))
	for _, section := range sections {
		for _, body := range chunkSection(section.body, target, max) {
			body = strings.TrimSpace(body)
			if body == "" {
				continue
			}
			chunks = append(chunks, ChunkDraft{
				ChunkIndex:  uint32(len(chunks)),
				Section:     section.title,
				Content:     body,
				ContentHash: hashContent([]byte(body)),
			})
		}
	}
	if len(chunks) == 0 {
		return nil, errors.New("empty content")
	}
	return chunks, nil
}

func NormalizeText(value string) string {
	value = strings.ReplaceAll(value, "\x00", "")
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	lines := strings.Split(value, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	value = strings.Join(lines, "\n")
	value = blankLinesPattern.ReplaceAllString(value, "\n\n")
	return strings.TrimSpace(value)
}

func (c Chunker) limits() (int, int) {
	target := c.TargetChars
	if target <= 0 {
		target = 900
	}
	max := c.MaxChars
	if max <= 0 {
		max = 1200
	}
	if target > max {
		target = max
	}
	return target, max
}

type textSection struct {
	title string
	body  string
}

func splitSections(fileName, text string) []textSection {
	fallback := strings.TrimSpace(filepath.Base(fileName))
	if fallback == "" || fallback == "." {
		fallback = "document"
	}
	currentTitle := fallback
	var current []string
	sections := make([]textSection, 0, 4)

	flush := func() {
		body := strings.TrimSpace(strings.Join(current, "\n"))
		if body != "" {
			sections = append(sections, textSection{title: currentTitle, body: body})
		}
		current = current[:0]
	}

	for _, line := range strings.Split(text, "\n") {
		if title, ok := markdownHeading(line); ok {
			flush()
			currentTitle = title
			continue
		}
		current = append(current, line)
	}
	flush()
	return sections
}

func markdownHeading(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "#") {
		return "", false
	}
	hashes := 0
	for hashes < len(trimmed) && trimmed[hashes] == '#' {
		hashes++
	}
	if hashes == len(trimmed) || trimmed[hashes] != ' ' {
		return "", false
	}
	title := strings.TrimSpace(trimmed[hashes:])
	return title, title != ""
}

func chunkSection(body string, target, max int) []string {
	paragraphs := strings.Split(body, "\n\n")
	chunks := make([]string, 0, len(paragraphs))
	var current string

	flush := func() {
		if strings.TrimSpace(current) != "" {
			chunks = append(chunks, current)
		}
		current = ""
	}

	for _, paragraph := range paragraphs {
		paragraph = strings.TrimSpace(paragraph)
		if paragraph == "" {
			continue
		}
		if len(paragraph) > max {
			flush()
			chunks = append(chunks, hardSplit(paragraph, max)...)
			continue
		}
		if current == "" {
			current = paragraph
			continue
		}
		candidate := current + "\n\n" + paragraph
		if len(candidate) <= target {
			current = candidate
			continue
		}
		flush()
		current = paragraph
	}
	flush()
	return chunks
}

func hardSplit(value string, max int) []string {
	parts := make([]string, 0, len(value)/max+1)
	for len(value) > max {
		parts = append(parts, value[:max])
		value = strings.TrimSpace(value[max:])
	}
	if value != "" {
		parts = append(parts, value)
	}
	return parts
}
