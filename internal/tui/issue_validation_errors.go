package tui

import "fmt"

// errRequiredDate reports a missing Linear date input.
func errRequiredDate() error {
	return fmt.Errorf("date is required")
}

// errInvalidDate reports a malformed Linear date input.
func errInvalidDate() error {
	return fmt.Errorf("date must be YYYY-MM-DD")
}

// errRequiredEstimate reports a missing estimate input.
func errRequiredEstimate() error {
	return fmt.Errorf("estimate is required")
}

// errInvalidEstimate reports a non-numeric estimate input.
func errInvalidEstimate() error {
	return fmt.Errorf("estimate must be numeric")
}

// errNegativeEstimate reports a negative estimate input.
func errNegativeEstimate() error {
	return fmt.Errorf("estimate must be non-negative")
}
