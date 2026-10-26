package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelModelEventsCanBeFilteredAndCleanedUp(t *testing.T) {
	require.NoError(t, DB.Exec("DELETE FROM channel_model_events").Error)
	t.Cleanup(func() { DB.Exec("DELETE FROM channel_model_events") })
	events := []ChannelModelEvent{
		{ChannelId: 1, ChannelName: "east", Model: "model-a", Event: ChannelModelEventDisabled, StatusCode: 429, CreatedAt: 100},
		{ChannelId: 1, ChannelName: "east", Model: "model-a", Event: ChannelModelEventProbeSuccess, SuccessCount: 1, CreatedAt: 200},
		{ChannelId: 2, ChannelName: "west", Model: "model-b", Event: ChannelModelEventRecovered, CreatedAt: 300},
	}
	for index := range events {
		require.NoError(t, RecordChannelModelEvent(&events[index]))
	}

	filtered, total, err := GetChannelModelEvents(ChannelModelEventQuery{
		ChannelId: 1, Model: "model-a", Event: ChannelModelEventProbeSuccess, StartTime: 150, EndTime: 250, Limit: 20,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, filtered, 1)
	assert.Equal(t, 1, filtered[0].SuccessCount)

	deleted, err := CleanupChannelModelEvents(250)
	require.NoError(t, err)
	assert.Equal(t, int64(2), deleted)
	remaining, total, err := GetChannelModelEvents(ChannelModelEventQuery{Limit: 20})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, remaining, 1)
	assert.Equal(t, ChannelModelEventRecovered, remaining[0].Event)
}

func TestCapabilityGateEventsCanBeFilteredAndCleanedUp(t *testing.T) {
	require.NoError(t, DB.Exec("DELETE FROM capability_gate_events").Error)
	t.Cleanup(func() { DB.Exec("DELETE FROM capability_gate_events") })
	events := []CapabilityGateEvent{
		{Mode: "shadow", Model: "model-a", Action: CapabilityGateEventWouldReject, RejectedChannels: "1,2", Dimensions: `[{"dimension":"tools.supported","severity":"hard"}]`, CreatedAt: 100},
		{Mode: "enforce", Model: "model-a", Action: CapabilityGateEventRejected, RejectedChannels: "3", Dimensions: `[{"dimension":"params.top_p","severity":"soft"}]`, CreatedAt: 200},
		{Mode: "enforce", Model: "model-b", Action: CapabilityGateEventSoft, Dimensions: `[{"dimension":"params.stop","severity":"soft"}]`, CreatedAt: 300},
	}
	for index := range events {
		require.NoError(t, RecordCapabilityGateEvent(&events[index]))
	}

	filtered, total, err := GetCapabilityGateEvents(CapabilityGateEventQuery{
		Model: "model-a", Action: CapabilityGateEventRejected, StartTime: 150, Limit: 20,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, filtered, 1)
	assert.Equal(t, "3", filtered[0].RejectedChannels)

	deleted, err := CleanupCapabilityGateEvents(250)
	require.NoError(t, err)
	assert.Equal(t, int64(2), deleted)
	remaining, total, err := GetCapabilityGateEvents(CapabilityGateEventQuery{Limit: 20})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, remaining, 1)
	assert.Equal(t, CapabilityGateEventSoft, remaining[0].Action)
}
