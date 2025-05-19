package helpers

// toStrings converts a []error into a []string for easy comparison
func ToStrings(errs []error) []string {
	if errs == nil {
		return nil
	}
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.Error()
	}
	return out
}
