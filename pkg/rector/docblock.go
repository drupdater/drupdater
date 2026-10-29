package rector

import "strings"

// fileDocBlockFirst moves a file's @file docblock back above the use statements Rector imported
// in front of it. In a file without a namespace Rector puts new imports first, ahead of the
// docblock, and Drupal's coding standard then has PHPCBF add a second, empty @file docblock.
func fileDocBlockFirst(src string) string {
	lines := strings.SplitAfter(src, "\n")
	if strings.TrimSpace(lines[0]) != "<?php" {
		return src
	}

	i := skipBlank(lines, 1)
	useStart := i
	for i < len(lines) && isUse(lines[i]) {
		i++
	}
	if i == useStart {
		return src
	}
	useEnd := i

	docStart := skipBlank(lines, i)
	if docStart == len(lines) || !strings.HasPrefix(strings.TrimSpace(lines[docStart]), "/**") {
		return src
	}
	docEnd := docStart
	for docEnd < len(lines) && !strings.HasSuffix(strings.TrimSpace(lines[docEnd]), "*/") {
		docEnd++
	}
	if docEnd == len(lines) {
		return src
	}
	docEnd++
	if !strings.Contains(strings.Join(lines[docStart:docEnd], ""), "@file") {
		return src
	}

	newline := "\n"
	if strings.HasSuffix(lines[0], "\r\n") {
		newline = "\r\n"
	}

	var out strings.Builder
	out.WriteString(lines[0])
	out.WriteString(newline)
	out.WriteString(strings.Join(lines[docStart:docEnd], ""))
	out.WriteString(newline)
	out.WriteString(strings.Join(lines[useStart:useEnd], ""))
	out.WriteString(strings.Join(lines[docEnd:], ""))
	return out.String()
}

func skipBlank(lines []string, i int) int {
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	return i
}

func isUse(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "use ") && strings.HasSuffix(trimmed, ";")
}
