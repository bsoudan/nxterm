package tui

import (
	"context"
	"log/slog"
	"sync"

	tea "charm.land/bubbletea/v2"
)

// Handle is the synchronous interface given to a task goroutine.
// Methods block until the bubbletea event loop processes the request.
type Handle struct {
	ctx    context.Context
	cancel context.CancelFunc
	outbox chan<- taskMsg
	inbox  chan any
	id     uint64
}

// Context returns the task's context, cancelled when the task is stopped.
func (h *Handle) Context() context.Context { return h.ctx }

// WaitFor blocks until filter returns deliver=true for an incoming message.
// The filter runs on the bubbletea goroutine for each message:
//   - deliver=true, handled=true:  task gets the message, layers don't
//   - deliver=true, handled=false: task gets it, layers also see it
//   - deliver=false, handled=false: not relevant, skip
func (h *Handle) WaitFor(filter func(msg any) (deliver, handled bool)) (any, error) {
	if err := h.send(taskWaitForMsg{taskID: h.id, filter: filter}); err != nil {
		return nil, err
	}
	return h.recv()
}

// Send sends a message to the bubbletea event loop and blocks until
// the app delivers a response via TaskRunner.Deliver. This ensures
// the payload is processed on the bubbletea goroutine, avoiding
// concurrent access to shared state.
func (h *Handle) Send(msg any) (any, error) {
	if err := h.send(taskSendMsg{taskID: h.id, payload: msg}); err != nil {
		return nil, err
	}
	return h.recv()
}

// PushLayer pushes a layer onto the UI stack.
func (h *Handle) PushLayer(layer Layer) {
	h.send(taskPushLayerMsg{layer: layer})
}

// PopLayer removes a layer from the UI stack.
func (h *Handle) PopLayer(layer Layer) {
	h.send(taskPopLayerMsg{layer: layer})
}

func (h *Handle) send(msg taskMsg) error {
	select {
	case h.outbox <- msg:
		return nil
	case <-h.ctx.Done():
		return h.ctx.Err()
	}
}

func (h *Handle) recv() (any, error) {
	select {
	case v := <-h.inbox:
		return v, nil
	case <-h.ctx.Done():
		return nil, h.ctx.Err()
	}
}

// taskMsg is the interface for messages sent from task goroutines to bubbletea.
type taskMsg interface {
	isTaskMsg()
}

// IsTaskMsg reports whether msg is an internal task message that should
// be routed to TaskRunner.HandleMsg.
func IsTaskMsg(msg tea.Msg) bool {
	_, ok := msg.(taskMsg)
	return ok
}

type taskWaitForMsg struct {
	taskID uint64
	filter func(any) (deliver, handled bool)
}

type taskPushLayerMsg struct {
	layer Layer
}

type taskPopLayerMsg struct {
	layer Layer
}

type taskSendMsg struct {
	taskID  uint64
	payload any
}

type taskDoneMsg struct {
	taskID uint64
}

func (taskWaitForMsg) isTaskMsg()  {}
func (taskSendMsg) isTaskMsg()     {}
func (taskPushLayerMsg) isTaskMsg() {}
func (taskPopLayerMsg) isTaskMsg()  {}
func (taskDoneMsg) isTaskMsg()     {}

// TaskSendMsg is delivered to the app when a task calls Handle.Send().
// The app processes Payload on the bubbletea goroutine (safe for shared
// state access) and calls TaskRunner.Deliver(TaskID, response) when done.
type TaskSendMsg struct {
	TaskID  uint64
	Payload any
}

// taskState tracks a running task's WaitFor filter.
type taskState struct {
	handle *Handle
	filter func(any) (deliver, handled bool)
}

// TaskRunner manages running tasks and bridges them to bubbletea.
type TaskRunner struct {
	fromTasks chan taskMsg
	nextID    uint64
	mu        sync.Mutex // protects tasks map
	tasks     map[uint64]*taskState
}

// NewTaskRunner creates a TaskRunner.
func NewTaskRunner() *TaskRunner {
	return &TaskRunner{
		fromTasks: make(chan taskMsg),
		tasks:     make(map[uint64]*taskState),
	}
}

// Run spawns a task goroutine. The function fn receives a Handle for
// synchronous communication with the bubbletea event loop.
func (r *TaskRunner) Run(fn func(*Handle)) uint64 {
	r.nextID++
	id := r.nextID

	ctx, cancel := context.WithCancel(context.Background())
	h := &Handle{
		ctx:    ctx,
		cancel: cancel,
		outbox: r.fromTasks,
		inbox:  make(chan any, 1),
		id:     id,
	}

	r.mu.Lock()
	r.tasks[id] = &taskState{handle: h}
	r.mu.Unlock()

	go func() {
		defer func() {
			if rv := recover(); rv != nil {
				slog.Debug("task panic recovered", "task", id, "panic", rv)
			}
			cancel()
			// Best-effort send; if outbox is closed or blocked, skip.
			select {
			case r.fromTasks <- taskDoneMsg{taskID: id}:
			default:
			}
		}()
		fn(h)
	}()

	return id
}

// Cancel cancels a task by ID.
func (r *TaskRunner) Cancel(id uint64) {
	r.mu.Lock()
	ts, ok := r.tasks[id]
	r.mu.Unlock()
	if ok {
		ts.handle.cancel()
	}
}

// ListenCmd returns a tea.Cmd that blocks on the task channel.
// The app should call this from Init and after each task message delivery.
func (r *TaskRunner) ListenCmd() tea.Cmd {
	ch := r.fromTasks
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// CheckFilters runs active WaitFor filters against msg. If a filter
// matches (deliver=true), the message is sent to the task's inbox and
// the filter is cleared. Returns handled=true if the message should
// not be passed to layers.
func (r *TaskRunner) CheckFilters(msg any) (handled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, ts := range r.tasks {
		if ts.filter == nil {
			continue
		}
		deliver, h := ts.filter(msg)
		if deliver {
			ts.filter = nil
			select {
			case ts.handle.inbox <- msg:
			case <-ts.handle.ctx.Done():
			}
			if h {
				return true
			}
		}
	}
	return false
}

// Deliver sends a response to a task that is blocked in Handle.Send().
// Must be called on the bubbletea goroutine.
func (r *TaskRunner) Deliver(taskID uint64, payload any) {
	r.mu.Lock()
	ts, ok := r.tasks[taskID]
	r.mu.Unlock()
	if ok {
		select {
		case ts.handle.inbox <- payload:
		case <-ts.handle.ctx.Done():
		}
	}
}

// HandleMsg processes a task message from the outbox channel. Returns
// a tea.Cmd for the app to execute (e.g. push/pop layer), or nil.
// The app should call ListenCmd again after handling each message.
func (r *TaskRunner) HandleMsg(msg tea.Msg) tea.Cmd {
	tmsg, ok := msg.(taskMsg)
	if !ok {
		return nil
	}

	switch msg := tmsg.(type) {
	case taskWaitForMsg:
		r.mu.Lock()
		if ts, ok := r.tasks[msg.taskID]; ok {
			ts.filter = msg.filter
		}
		r.mu.Unlock()
		return nil

	case taskSendMsg:
		return func() tea.Msg {
			return TaskSendMsg{TaskID: msg.taskID, Payload: msg.payload}
		}

	case taskPushLayerMsg:
		return func() tea.Msg { return PushLayerMsg{Layer: msg.layer} }

	case taskPopLayerMsg:
		layer := msg.layer
		return func() tea.Msg { return popLayerMsg{layer: layer} }

	case taskDoneMsg:
		r.mu.Lock()
		delete(r.tasks, msg.taskID)
		r.mu.Unlock()
		return nil
	}
	return nil
}

// DriveOne reads one message from the outbox and processes it.
// For testing only — simulates what the app's Update does.
func (r *TaskRunner) DriveOne() tea.Msg {
	msg := <-r.fromTasks
	r.HandleMsg(msg)
	return msg
}
