// Package bus provides a generic in-process event bus with topic-based
// pub/sub semantics:
//
//   - On registers a persistent handler for a topic.
//   - Once registers a handler that is auto-removed after its first dispatch.
//   - Off removes one or all handlers for a topic.
//   - Trigger dispatches a topic to its handlers (and to "*" handlers when
//     AllowAsterisk is set).
//   - Broadcast dispatches to every registered handler.
//
// Bus[T] is generic over the event payload type T. Handlers implement
// Event[T]. The bus is safe for concurrent use.
package bus
