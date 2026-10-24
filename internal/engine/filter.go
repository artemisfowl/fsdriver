package engine

import "strings"

type FilterRules struct {
	IncludePatterns []string `json:"include_patterns,omitempty"`
	ExcludePatterns []string `json:"exclude_patterns,omitempty"`
}

// Check if a given line passes the filter criteria
func (f *FilterRules) Matches(line string) bool {
	// if no filters are provided at all, allow every line
	if len(f.IncludePatterns) == 0 && len(f.ExcludePatterns) == 0 {
		return true
	}

	if len(f.IncludePatterns) > 0 {
		matched := false
		for _, pattern := range f.IncludePatterns {
			if strings.Contains(line, pattern) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// if exclude patterns are specified, the line must adhere to this rule as
	// well
	for _, pattern := range f.ExcludePatterns {
		if strings.Contains(line, pattern) {
			return false
		}
	}
	return true
}
