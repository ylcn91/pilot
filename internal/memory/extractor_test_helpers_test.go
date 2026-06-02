package memory

// patternTitles returns the titles of extracted patterns for test diagnostics.
func patternTitles(patterns []*ExtractedPattern) []string {
	titles := make([]string, len(patterns))
	for i, p := range patterns {
		titles[i] = p.Title
	}
	return titles
}
