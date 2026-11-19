package process

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAwaitGroupGivesUpOnARunThatOutlivesItsKill(t *testing.T) {
	limit := 50 * time.Millisecond
	grace := 50 * time.Millisecond
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	started := time.Now()

	result := awaitGroup(limit, grace, func(context.Context) Result {
		<-release
		return Result{}
	})

	assert.True(t, result.TimedOut)
	assert.True(t, result.Leftover)
	require.ErrorContains(t, result.Err, "still running 50ms past its 50ms limit")
	assert.Less(t, time.Since(started), time.Second)
}

func TestAwaitGroupReturnsWhatAFinishedRunGives(t *testing.T) {
	want := Result{Stdout: "n/a\n"}

	result := awaitGroup(time.Second, time.Second, func(context.Context) Result { return want })

	assert.Equal(t, want, result)
}

func TestAwaitGroupCancelsTheRunAtItsLimit(t *testing.T) {
	result := awaitGroup(50*time.Millisecond, time.Second, func(ctx context.Context) Result {
		<-ctx.Done()
		return Result{TimedOut: true, Err: ctx.Err()}
	})

	assert.True(t, result.TimedOut)
	assert.False(t, result.Leftover)
	require.ErrorIs(t, result.Err, context.DeadlineExceeded)
}
