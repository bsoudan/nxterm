# pkg/layer

Generic compositable layer stack and task runner for building terminal UIs on bubbletea.

## Layer Interface

```go
type Layer[RS any] interface {
    Activate() tea.Cmd
    Deactivate()
    Update(tea.Msg) (response tea.Msg, cmd tea.Cmd, handled bool)
    View(width, height int, rs *RS) []*lipgloss.Layer
}
```

`RS` is an application-defined render state (e.g., `RenderState` in the TUI). Layers are mutable pointers — `Update` mutates in place.

## Stack

`Stack[RS]` manages an ordered layer stack with top-down message dispatch:
- Base layer (index 0) is permanent
- `Push(l)` adds to top, calls `Activate()`
- `Pop(l)` removes and calls `Deactivate()`
- `Update(msg)` dispatches top-down; first layer returning `handled=true` stops propagation
- `View()` composites all layers bottom-up, collecting `[]*lipgloss.Layer`

Layers return `QuitLayerMsg` from `Update()` to request self-removal.

## TaskRunner

Bridges synchronous goroutine code to bubbletea's async event loop.

### Handle[RS]
Given to task goroutines for blocking communication:
- `WaitFor(filter)` — blocks until a message matches the filter. Single-shot.
- `Subscribe(filter)` — persistent filter, delivers to channel. Returns unsubscribe function.
- `Send(msg)` — sends payload to bubbletea goroutine, blocks for response
- `PushLayer(l)` / `PopLayer(l)` — manage UI layers from within a task
- `Context()` — cancelled when task is stopped

### TaskRunner[RS]
- `Run(fn)` — spawns a task goroutine with a `Handle`
- `Cancel(id)` — cancels a task by ID
- `ListenCmd()` — tea.Cmd that blocks on task channel (call from Init + after each delivery)
- `CheckFilters(msg)` — runs active WaitFor/Subscribe filters against incoming messages
- `Deliver(id, response)` — delivers response to a task's `Send()` call
- `HandleMsg(msg)` — processes internal task messages (waitFor, push/pop, subscribe, done)

Tasks enable sequential multi-step workflows (e.g., upgrade: confirm → monitor → download → swap) without scattering logic across Update callbacks.
