package util

const defaultMaxIDLength = 128

func ValidateID(id string) bool {
	return ValidateIDWithLimit(id, defaultMaxIDLength)
}

func ValidateIDWithLimit(id string, maxLen int) bool {
	if maxLen <= 0 {
		maxLen = defaultMaxIDLength
	}
	if id == "" || len(id) > maxLen {
		return false
	}
	for _, c := range id {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}
