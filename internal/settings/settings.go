package settings

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"time"
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

func PositiveDuration(name string, fallback int, unit time.Duration, units string) (int, error) {
	number, err := PositiveInt(name, fallback)
	if err != nil {
		return 0, err
	}
	if most := math.MaxInt64 / int64(unit); int64(number) > most {
		return 0, fmt.Errorf("%s must be at most %d %s, got: %d", name, most, units, number)
	}
	return number, nil
}
