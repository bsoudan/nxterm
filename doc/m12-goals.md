# Milestone 12 Goals — Layer Architecture

## 1. Layer-based message routing

Replace the monolithic Update() switch with a stack of layers. Each layer
implements the same interface as a bubbletea model: Update(msg) returns
(layer, cmd). Messages go to every layer. Layers return commands that produce
new messages, feeding back through the stack — same as bubbletea's own loop.

## 2. Dynamic layer stack

The stack is dynamic. Session is the root (always present). Terminal is pushed
when the first region connects. Temporary layers (prefix, hint, overlays,
scrollback) are pushed and popped by user actions or timers.

## 3. Localized state

Each layer owns its state. The session layer owns connection/reconnect/region
state. The terminal layer owns the screen/cursor/capabilities. Overlays own
their viewport/cursor. No reaching across layers for data.

## 4. Composited rendering

View() composes bottom-up: each layer renders over the layer below it.
The top layer's Label() sets the tab bar status text.
