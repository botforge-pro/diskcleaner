package xcode

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDerivedDataFollowsXcodesOwnLocation(t *testing.T) {
	custom := func(string) string { return "/Volumes/Fast/DerivedData" }
	none := func(string) string { return "" }

	assert.Equal(t, "/Volumes/Fast/DerivedData", DerivedData("/home", custom))
	assert.Equal(t, "/home/Library/Developer/Xcode/DerivedData", DerivedData("/home", none))
}
