package filter

import (
	"strings"
)

// Filter is an interface for data filtering
type Filter interface {
	Process(input string) string
}

// Chain represents a chain of filters that will be applied in sequence
type Chain struct {
	filters []Filter
}

// NewChain creates a new filter chain
func NewChain(filters ...Filter) *Chain {
	return &Chain{
		filters: filters,
	}
}

// AddFilter adds a filter to the chain
func (c *Chain) AddFilter(filter Filter) {
	c.filters = append(c.filters, filter)
}

// Process applies all filters in the chain to the input
func (c *Chain) Process(input string) string {
	result := input
	for _, filter := range c.filters {
		result = filter.Process(result)
	}
	return result
}

// SensitiveDataFilter implements Filter for sensitive data removal
type SensitiveDataFilter struct {
	disabled bool
}

// NewSensitiveDataFilter creates a new sensitive data filter
func NewSensitiveDataFilter(disabled bool) *SensitiveDataFilter {
	return &SensitiveDataFilter{
		disabled: disabled,
	}
}

// Process applies the sensitive data filter
func (f *SensitiveDataFilter) Process(input string) string {
	if f.disabled {
		return input
	}
	return SensitiveData(input)
}

// RegexReplacementFilter implements a regex-based replacement filter
type RegexReplacementFilter struct {
	patterns map[string]string
}

// NewRegexReplacementFilter creates a new regex replacement filter
func NewRegexReplacementFilter() *RegexReplacementFilter {
	return &RegexReplacementFilter{
		patterns: make(map[string]string),
	}
}

// AddPattern adds a pattern to replace in the input
func (f *RegexReplacementFilter) AddPattern(pattern, replacement string) {
	f.patterns[pattern] = replacement
}

// Process applies regex replacements
func (f *RegexReplacementFilter) Process(input string) string {
	result := input
	for pattern, replacement := range f.patterns {
		result = strings.ReplaceAll(result, pattern, replacement)
	}
	return result
}

// LineFilter implements a filter that works on individual lines
type LineFilter struct {
	predicate func(line string) bool
}

// NewLineFilter creates a new line filter with the given predicate
func NewLineFilter(predicate func(line string) bool) *LineFilter {
	return &LineFilter{
		predicate: predicate,
	}
}

// Process filters lines based on the predicate
func (f *LineFilter) Process(input string) string {
	lines := strings.Split(input, "\n")
	var result []string

	for _, line := range lines {
		if f.predicate(line) {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}
