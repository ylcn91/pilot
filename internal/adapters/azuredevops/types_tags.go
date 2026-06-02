package azuredevops

// Helper functions

// splitTags splits a semicolon-separated tag string into a slice
func splitTags(tags string) []string {
	if tags == "" {
		return nil
	}
	var result []string
	for _, tag := range splitAndTrim(tags, ";") {
		if tag != "" {
			result = append(result, tag)
		}
	}
	return result
}

// splitAndTrim splits a string and trims whitespace from each part
func splitAndTrim(s string, sep string) []string {
	parts := make([]string, 0)
	current := ""
	for i := 0; i < len(s); i++ {
		if i+len(sep) <= len(s) && s[i:i+len(sep)] == sep {
			trimmed := trimSpace(current)
			if trimmed != "" {
				parts = append(parts, trimmed)
			}
			current = ""
			i += len(sep) - 1
		} else {
			current += string(s[i])
		}
	}
	if trimmed := trimSpace(current); trimmed != "" {
		parts = append(parts, trimmed)
	}
	return parts
}

// trimSpace trims leading and trailing whitespace
func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

// joinTags joins tags into a semicolon-separated string
func joinTags(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	result := tags[0]
	for i := 1; i < len(tags); i++ {
		result += "; " + tags[i]
	}
	return result
}

// addTag adds a tag to the tags string if not already present
func addTag(tagsStr, newTag string) string {
	tags := splitTags(tagsStr)
	for _, t := range tags {
		if t == newTag {
			return tagsStr // Already has tag
		}
	}
	tags = append(tags, newTag)
	return joinTags(tags)
}

// removeTag removes a tag from the tags string
func removeTag(tagsStr, tagToRemove string) string {
	tags := splitTags(tagsStr)
	result := make([]string, 0, len(tags))
	for _, t := range tags {
		if t != tagToRemove {
			result = append(result, t)
		}
	}
	return joinTags(result)
}
