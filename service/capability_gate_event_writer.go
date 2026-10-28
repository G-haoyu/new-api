package service

import (
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const capabilityGateEventStopTimeout = 10 * time.Second

// capabilityGateEventWriter 把门控事件批量写进日志库。
//
// 投递永远不阻塞请求：队列满了就丢弃并计数，遥测积压不能变成路由反压。
// 一次批量写入失败只记日志，不重试，也不影响后续批次。
type capabilityGateEventWriter struct {
	queue         chan *model.CapabilityGateEvent
	batchSize     int
	flushInterval time.Duration
	insert        func([]*model.CapabilityGateEvent) error

	dropped atomic.Int64
	stop    chan struct{}
	done    chan struct{}
}

// capabilityGateEvents 是进程唯一的事件 writer。
// 没有启动 writer 的入口（命令行子命令、单元测试）产生的事件会被直接丢弃。
var capabilityGateEvents atomic.Pointer[capabilityGateEventWriter]

func newCapabilityGateEventWriter(queueSize, batchSize int, flushInterval time.Duration, insert func([]*model.CapabilityGateEvent) error) *capabilityGateEventWriter {
	writer := &capabilityGateEventWriter{
		queue:         make(chan *model.CapabilityGateEvent, queueSize),
		batchSize:     batchSize,
		flushInterval: flushInterval,
		insert:        insert,
		stop:          make(chan struct{}),
		done:          make(chan struct{}),
	}
	go writer.run()
	return writer
}

// StartCapabilityGateEventWriter 启动事件 writer，重复调用无副作用。
//
// 三个参数由环境变量调整，写法与 pkg/attemptlog/config.go 一致，超出范围的值会被截断：
//   - CAPABILITY_GATE_EVENT_BUFFER_SIZE  队列容量，默认 4096，范围 1-1000000
//   - CAPABILITY_GATE_EVENT_BATCH_SIZE   每批写入条数，默认 200，范围 1-10000，且不超过队列容量
//   - CAPABILITY_GATE_EVENT_FLUSH_MS     批量写入的最长等待毫秒数，默认 2000，范围 100-3600000
func StartCapabilityGateEventWriter() {
	bufferSize := min(max(common.GetEnvOrDefault("CAPABILITY_GATE_EVENT_BUFFER_SIZE", 4096), 1), 1000000)
	batchSize := min(max(common.GetEnvOrDefault("CAPABILITY_GATE_EVENT_BATCH_SIZE", 200), 1), 10000, bufferSize)
	flushMillis := min(max(common.GetEnvOrDefault("CAPABILITY_GATE_EVENT_FLUSH_MS", 2000), 100), 3600000)
	writer := newCapabilityGateEventWriter(bufferSize, batchSize,
		time.Duration(flushMillis)*time.Millisecond, model.BatchInsertCapabilityGateEvents)
	if !capabilityGateEvents.CompareAndSwap(nil, writer) {
		writer.shutdown(capabilityGateEventStopTimeout)
	}
}

// StopCapabilityGateEventWriter 停止 writer 并把队列里剩余的事件写完。
// 停止之后到达的事件会被丢弃，进程退出前调用即可。
func StopCapabilityGateEventWriter() {
	if writer := capabilityGateEvents.Swap(nil); writer != nil {
		writer.shutdown(capabilityGateEventStopTimeout)
	}
}

func (w *capabilityGateEventWriter) enqueue(event *model.CapabilityGateEvent) {
	select {
	case w.queue <- event:
	default:
		if n := w.dropped.Add(1); n == 1 || n%1000 == 0 {
			common.SysError(common.LogText("capability gate event queue is full, dropped %d events so far", n))
		}
	}
}

func (w *capabilityGateEventWriter) run() {
	defer close(w.done)
	ticker := time.NewTicker(w.flushInterval)
	defer ticker.Stop()

	batch := make([]*model.CapabilityGateEvent, 0, w.batchSize)
	for {
		select {
		case event := <-w.queue:
			batch = append(batch, event)
			if len(batch) < w.batchSize {
				continue
			}
		case <-ticker.C:
		case <-w.stop:
			for len(w.queue) > 0 {
				batch = append(batch, <-w.queue)
			}
			w.flush(batch)
			return
		}
		w.flush(batch)
		batch = batch[:0]
	}
}

func (w *capabilityGateEventWriter) flush(batch []*model.CapabilityGateEvent) {
	if len(batch) == 0 {
		return
	}
	// 遥测写入的任何失败（包括 panic）都不能带倒网关进程。
	defer func() {
		if r := recover(); r != nil {
			common.SysError(common.LogText("capability gate event writer panicked: %v", r))
		}
	}()
	if err := w.insert(batch); err != nil {
		common.SysError(common.LogText("failed to write %d capability gate events: %v", len(batch), err))
	}
}

// shutdown 通知 run 把剩余事件写完后退出，最多等待 timeout，防止数据库挂死拖住进程退出。
func (w *capabilityGateEventWriter) shutdown(timeout time.Duration) {
	close(w.stop)
	select {
	case <-w.done:
	case <-time.After(timeout):
		common.SysError(common.LogText("timed out waiting for the capability gate event writer to drain"))
	}
}
