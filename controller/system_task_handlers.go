package controller

import (
	"context"
	"os"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/autorouter/modelrank"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/autorouter_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// RegisterScheduledSystemTasks wires the periodic channel test, upstream model
// update, auto router ranking and async task polling (Midjourney / Suno /
// video) jobs into the
// system task framework so a DB lease dedups execution across multiple master
// instances and each run is recorded as one task row. Call this before
// service.StartSystemTaskRunner.
func RegisterScheduledSystemTasks() {
	service.RegisterSystemTaskHandler(channelTestHandler{})
	service.RegisterSystemTaskHandler(channelModelRecoveryHandler{})
	service.RegisterSystemTaskHandler(modelUpdateHandler{})
	service.RegisterSystemTaskHandler(midjourneyPollHandler{})
	service.RegisterSystemTaskHandler(asyncTaskPollHandler{})
	service.RegisterSystemTaskHandler(autoRouterRankHandler{})
}

// autoRouterRankHandler regenerates the auto router's task → model lists from
// public leaderboards. One node builds them under the task lease and writes
// the options; the others pick them up through the option sync, so a new
// ranking takes effect everywhere without a restart.
type autoRouterRankHandler struct{}

func (autoRouterRankHandler) Type() string { return model.SystemTaskTypeAutoRouterRank }

func (autoRouterRankHandler) Enabled() bool {
	setting := autorouter_setting.GetAutoRouterSetting()
	return setting.Enabled && setting.RankRefreshEnabled && os.Getenv("OPENROUTER_API_KEY") != ""
}

func (autoRouterRankHandler) Interval() time.Duration {
	return time.Duration(max(autorouter_setting.GetAutoRouterSetting().RankRefreshIntervalHours, 1)) * time.Hour
}

func (autoRouterRankHandler) NewPayload() any { return nil }

// Run leaves the current lists in place when any source fails, so a failed
// refresh only delays the next ranking. rank_sources is written last and
// marks a complete update.
func (autoRouterRankHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	cfg := modelrank.DefaultConfig()
	cfg.APIKey = os.Getenv("OPENROUTER_API_KEY")
	mapping, err := modelrank.Build(ctx, cfg)
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	options := []struct {
		key   string
		value any
	}{
		{"autorouter_setting.task_models", mapping.TaskModels},
		{"autorouter_setting.default_models", mapping.DefaultModels},
		{"autorouter_setting.rank_sources", mapping.Sources},
	}
	for _, option := range options {
		value, err := common.Marshal(option.value)
		if err == nil {
			err = model.UpdateOption(option.key, string(value))
		}
		if err != nil {
			finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
			return
		}
	}
	summary := map[string]any{"rank_sources": mapping.Sources, "default_models": len(mapping.DefaultModels)}
	for taskType, models := range mapping.TaskModels {
		summary[taskType] = map[string]any{"models": len(models), "top": models[:min(len(models), 3)]}
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

type channelModelRecoveryHandler struct{}

func (channelModelRecoveryHandler) Type() string {
	return model.SystemTaskTypeChannelModelRecovery
}

func (channelModelRecoveryHandler) Enabled() bool {
	setting := operation_setting.GetMonitorSetting()
	now := common.GetTimestamp()
	retentionDays := setting.ChannelModelEventRetentionDays
	if retentionDays < 1 {
		retentionDays = operation_setting.DefaultChannelModelEventRetentionDays
	}
	cleanupDue := model.HasChannelModelEventsBefore(now - int64(retentionDays)*86400)
	if !setting.ChannelModelCircuitBreakerEnabled || !setting.ChannelModelRecoveryEnabled {
		return cleanupDue
	}
	return cleanupDue || model.HasDueChannelModelRecovery(
		now,
		int64(setting.ChannelModelRecoveryDelayMinutes)*60,
		int64(setting.ChannelModelRecoveryIntervalMinutes)*60,
	)
}

func (channelModelRecoveryHandler) Interval() time.Duration {
	minutes := operation_setting.GetMonitorSetting().ChannelModelRecoveryIntervalMinutes
	if minutes < 1 {
		minutes = operation_setting.DefaultChannelModelRecoveryIntervalMinutes
	}
	return time.Duration(minutes) * time.Minute
}

func (channelModelRecoveryHandler) NewPayload() any { return nil }

func (channelModelRecoveryHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	summary, err := runChannelModelRecoveryTask(ctx, service.NewSystemTaskProgressReporter(task, runnerID))
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

// channelTestHandler runs the scheduled "test all channels" job. Enablement and
// cadence still come from the monitor settings; only the execution path moved
// into the system task runner.
type channelTestHandler struct{}

func (channelTestHandler) Type() string { return model.SystemTaskTypeChannelTest }

func (channelTestHandler) Enabled() bool {
	return operation_setting.GetMonitorSetting().AutoTestChannelEnabled
}

func (channelTestHandler) Interval() time.Duration {
	minutes := operation_setting.GetMonitorSetting().AutoTestChannelMinutes
	if minutes <= 0 {
		minutes = 10
	}
	return time.Duration(minutes * float64(time.Minute))
}

func (channelTestHandler) NewPayload() any { return nil }

// channelTestTaskPayload controls one channel_test run. A nil/empty payload is a
// scheduled run, which uses the configured monitor ChannelTestMode and does not
// notify. A manual "test all channels" trigger sets Mode=scheduled_all and
// Notify=true to reproduce the legacy manual behavior (test every channel and
// notify root on completion).
type channelTestTaskPayload struct {
	Mode   string `json:"mode,omitempty"`
	Notify bool   `json:"notify,omitempty"`
}

func (channelTestHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := channelTestTaskPayload{}
	if err := task.DecodePayload(&payload); err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	summary, err := runChannelTestTask(ctx, payload.Mode, payload.Notify, service.NewSystemTaskProgressReporter(task, runnerID))
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

// modelUpdateHandler runs the scheduled upstream model update detection job.
type modelUpdateHandler struct{}

func (modelUpdateHandler) Type() string { return model.SystemTaskTypeModelUpdate }

func (modelUpdateHandler) Enabled() bool {
	return common.GetEnvOrDefaultBool("CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_ENABLED", true)
}

func (modelUpdateHandler) Interval() time.Duration {
	intervalMinutes := common.GetEnvOrDefault(
		"CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_INTERVAL_MINUTES",
		channelUpstreamModelUpdateTaskDefaultIntervalMinutes,
	)
	if intervalMinutes < 1 {
		intervalMinutes = channelUpstreamModelUpdateTaskDefaultIntervalMinutes
	}
	return time.Duration(intervalMinutes) * time.Minute
}

func (modelUpdateHandler) NewPayload() any { return nil }

// modelUpdateTaskPayload controls one model_update run. A scheduled run
// (Manual=false) respects the per-channel minimum check interval and may
// auto-apply detected models when a channel has auto-sync enabled. A manual
// "detect all" trigger sets Manual=true to reproduce the legacy detect-all
// semantics: force a re-check regardless of the interval and never auto-apply,
// so the admin reviews and applies changes explicitly.
type modelUpdateTaskPayload struct {
	Manual bool `json:"manual,omitempty"`
}

func (modelUpdateHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := modelUpdateTaskPayload{}
	if err := task.DecodePayload(&payload); err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	summary := runChannelUpstreamModelUpdateTaskOnce(ctx, payload.Manual, !payload.Manual, service.NewSystemTaskProgressReporter(task, runnerID))
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

// midjourneyPollHandler runs one Midjourney polling pass per scheduled run.
// Enabled() folds the "are there unfinished tasks?" check into enablement so the
// scheduler creates no row when the system is idle; only when at least one
// Midjourney task is in progress does a row get scheduled.
type midjourneyPollHandler struct{}

func (midjourneyPollHandler) Type() string { return model.SystemTaskTypeMidjourneyPoll }

func (midjourneyPollHandler) Enabled() bool {
	return constant.UpdateTask && model.HasUnfinishedMidjourneyTasks()
}

func (midjourneyPollHandler) Interval() time.Duration { return 15 * time.Second }

func (midjourneyPollHandler) NewPayload() any { return nil }

func (midjourneyPollHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	summary := runMidjourneyTaskUpdateOnce(ctx, service.NewSystemTaskProgressReporter(task, runnerID))
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

// asyncTaskPollHandler runs one async-task (Suno/video) polling pass per
// scheduled run. Like midjourneyPollHandler, Enabled() folds in the unfinished
// task existence check so an idle system schedules no rows.
type asyncTaskPollHandler struct{}

func (asyncTaskPollHandler) Type() string { return model.SystemTaskTypeAsyncTaskPoll }

func (asyncTaskPollHandler) Enabled() bool {
	return constant.UpdateTask && model.HasUnfinishedSyncTasks()
}

func (asyncTaskPollHandler) Interval() time.Duration { return 15 * time.Second }

func (asyncTaskPollHandler) NewPayload() any { return nil }

func (asyncTaskPollHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	summary := service.RunTaskPollingOnce(ctx, service.NewSystemTaskProgressReporter(task, runnerID))
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

func finishSystemTaskHandler(task *model.SystemTask, runnerID string, status model.SystemTaskStatus, result any, runErr error) {
	errorMessage := ""
	if runErr != nil {
		errorMessage = runErr.Error()
	}
	if err := model.FinishSystemTask(task.TaskID, runnerID, status, result, errorMessage); err != nil {
		common.SysLog(common.LogText("system task %s failed to persist result: %v", task.TaskID, err))
	}
}
