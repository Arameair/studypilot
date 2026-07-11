package schema

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
)

type frontmatterLine struct {
	key, value string
	raw        []byte
}

// Markdown retains original lines and performs only targeted frontmatter and marker edits.
type Markdown struct {
	newline        []byte
	finalNewline   bool
	frontmatter    []frontmatterLine
	body           []byte
	managedFields  map[string]bool
	managedRegions map[string]bool
}

func ParseMarkdown(content []byte, managedFields, managedRegions []string) (*Markdown, error) {
	newline := []byte("\n")
	if bytes.Contains(content, []byte("\r\n")) {
		newline = []byte("\r\n")
	}
	firstEnd := bytes.Index(content, newline)
	if firstEnd < 0 || string(content[:firstEnd]) != "---" {
		return nil, fmt.Errorf("%w: missing frontmatter", ErrMalformedDocument)
	}
	closingNeedle := append(append([]byte(nil), newline...), []byte("---")...)
	closingNeedle = append(closingNeedle, newline...)
	closing := bytes.Index(content[firstEnd+len(newline):], closingNeedle)
	if closing < 0 {
		return nil, fmt.Errorf("%w: unterminated frontmatter", ErrMalformedDocument)
	}
	closing += firstEnd + len(newline)
	frontRaw := content[firstEnd+len(newline) : closing]
	bodyStart := closing + len(closingNeedle)
	lines := bytes.Split(frontRaw, newline)
	managed := make(map[string]bool, len(managedFields))
	for _, key := range managedFields {
		managed[key] = true
	}
	seenManaged := map[string]bool{}
	parsed := make([]frontmatterLine, 0, len(lines))
	for _, line := range lines {
		key, value := splitFrontmatterLine(line)
		if key != "" && managed[key] {
			if seenManaged[key] {
				return nil, fmt.Errorf("%w: %s", ErrDuplicateManagedKey, key)
			}
			seenManaged[key] = true
		}
		parsed = append(parsed, frontmatterLine{key: key, value: value, raw: slices.Clone(line)})
	}
	regions := make(map[string]bool, len(managedRegions))
	for _, region := range managedRegions {
		regions[region] = true
	}
	markdown := &Markdown{newline: newline, finalNewline: bytes.HasSuffix(content, newline), frontmatter: parsed, body: slices.Clone(content[bodyStart:]), managedFields: managed, managedRegions: regions}
	if err := markdown.validateRegions(); err != nil {
		return nil, err
	}
	return markdown, nil
}

func splitFrontmatterLine(line []byte) (string, string) {
	index := bytes.IndexByte(line, ':')
	if index <= 0 {
		return "", ""
	}
	return strings.TrimSpace(string(line[:index])), strings.TrimSpace(string(line[index+1:]))
}

func (m *Markdown) Value(key string) (string, bool) {
	for _, line := range m.frontmatter {
		if line.key == key {
			return line.value, true
		}
	}
	return "", false
}

func (m *Markdown) SetManagedField(key, value string) error {
	if !m.managedFields[key] {
		return fmt.Errorf("%w: unmanaged key %s", ErrMalformedDocument, key)
	}
	for index := range m.frontmatter {
		if m.frontmatter[index].key == key {
			m.frontmatter[index].value = value
			m.frontmatter[index].raw = []byte(key + ": " + value)
			return nil
		}
	}
	m.frontmatter = append(m.frontmatter, frontmatterLine{key: key, value: value, raw: []byte(key + ": " + value)})
	return nil
}

func (m *Markdown) RenameManagedField(oldKey, newKey string) error {
	if !m.managedFields[oldKey] || !m.managedFields[newKey] {
		return ErrMalformedDocument
	}
	if _, exists := m.Value(newKey); exists {
		return fmt.Errorf("%w: %s", ErrDuplicateManagedKey, newKey)
	}
	for index := range m.frontmatter {
		if m.frontmatter[index].key == oldKey {
			m.frontmatter[index].key = newKey
			m.frontmatter[index].raw = []byte(newKey + ": " + m.frontmatter[index].value)
			return nil
		}
	}
	return nil
}

func (m *Markdown) SetManagedRegion(name string, content []byte) error {
	if !m.managedRegions[name] {
		return ErrMalformedManagedRegion
	}
	begin := []byte("<!-- studypilot:begin " + name + " -->")
	end := []byte("<!-- studypilot:end " + name + " -->")
	start, finish := bytes.Index(m.body, begin), bytes.Index(m.body, end)
	if start < 0 && finish < 0 {
		if len(m.body) > 0 && !bytes.HasSuffix(m.body, m.newline) {
			m.body = append(m.body, m.newline...)
		}
		m.body = append(m.body, begin...)
		m.body = append(m.body, m.newline...)
		m.body = append(m.body, content...)
		if len(content) > 0 && !bytes.HasSuffix(content, m.newline) {
			m.body = append(m.body, m.newline...)
		}
		m.body = append(m.body, end...)
		if m.finalNewline {
			m.body = append(m.body, m.newline...)
		}
		return nil
	}
	if start < 0 || finish < start {
		return ErrMalformedManagedRegion
	}
	insideStart := start + len(begin)
	replacement := append([]byte(nil), m.body[:insideStart]...)
	replacement = append(replacement, m.newline...)
	replacement = append(replacement, content...)
	if len(content) > 0 && !bytes.HasSuffix(content, m.newline) {
		replacement = append(replacement, m.newline...)
	}
	replacement = append(replacement, m.body[finish:]...)
	m.body = replacement
	return nil
}

func (m *Markdown) Bytes() []byte {
	var output bytes.Buffer
	output.WriteString("---")
	output.Write(m.newline)
	for _, line := range m.frontmatter {
		output.Write(line.raw)
		output.Write(m.newline)
	}
	output.WriteString("---")
	output.Write(m.newline)
	output.Write(m.body)
	result := output.Bytes()
	if !m.finalNewline {
		result = bytes.TrimSuffix(result, m.newline)
	}
	return slices.Clone(result)
}

func (m *Markdown) validateRegions() error {
	stack := ""
	for _, line := range bytes.Split(m.body, m.newline) {
		text := string(bytes.TrimSpace(line))
		if strings.HasPrefix(text, "<!-- studypilot:begin ") && strings.HasSuffix(text, " -->") {
			name := strings.TrimSuffix(strings.TrimPrefix(text, "<!-- studypilot:begin "), " -->")
			if !m.managedRegions[name] || stack != "" {
				return ErrMalformedManagedRegion
			}
			stack = name
		} else if strings.HasPrefix(text, "<!-- studypilot:end ") && strings.HasSuffix(text, " -->") {
			name := strings.TrimSuffix(strings.TrimPrefix(text, "<!-- studypilot:end "), " -->")
			if stack != name {
				return ErrMalformedManagedRegion
			}
			stack = ""
		}
	}
	if stack != "" {
		return ErrMalformedManagedRegion
	}
	return nil
}
