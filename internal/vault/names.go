package vault

// ValidName checks project/key slug syntax.
func ValidName(field, s string) error {
	return validName(field, s)
}
