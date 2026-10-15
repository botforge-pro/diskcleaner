package settings

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
)

var wholeNumber = regexp.MustCompile(`^[1-9][0-9]*$`)

func Env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func PositiveInt(name string, fallback int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	number, err := strconv.Atoi(value)
	if !wholeNumber.MatchString(value) || err != nil {
		return 0, fmt.Errorf("%s must be a whole number above zero, got: %s", name, value)
	}
	return number, nil
}
