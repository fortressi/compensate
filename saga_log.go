package compensate

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// SagaNodeID represents a unique identifier for a saga node.
type SagaNodeID int

// SagaNodeEvent represents an entry in the saga log.
type SagaNodeEvent struct {
	SagaID    SagaID
	NodeID    SagaNodeID
	EventType SagaNodeEventType
	// TODO: Add timestamp field for more accurate event ordering?
}

// String implements the fmt.Stringer interface for SagaNodeEvent.
func (e *SagaNodeEvent) String() string {
	return fmt.Sprintf("N%03d %s", e.NodeID, e.EventType.String())
}

// SagaNodeEventType defines the types of events that can occur for a saga node.
type SagaNodeEventType int

const (
	EventStarted SagaNodeEventType = iota
	EventSucceeded
	EventFailed
	EventUndoStarted
	EventUndoFinished
	EventUndoFailed
)

// String returns the string representation of the SagaNodeEventType.
func (s SagaNodeEventType) String() string {
	switch s {
	case EventStarted:
		return "started"
	case EventSucceeded:
		return "succeeded"
	case EventFailed:
		return "failed"
	case EventUndoStarted:
		return "undo_started"
	case EventUndoFinished:
		return "undo_finished"
	case EventUndoFailed:
		return "undo_failed"
	default:
		return fmt.Sprintf("Unknown SagaNodeEventType: %d", s)
	}
}

// SagaNodeLoadStatus represents the persistent status for a saga node.
type SagaNodeLoadStatus int

const (
	LoadNeverStarted SagaNodeLoadStatus = iota
	LoadStarted
	LoadSucceeded
	LoadFailed
	LoadUndoStarted
	LoadUndoFinished
	LoadUndoFailed
)

// nextStatus returns the new status for a node after recording the given event.
func (s SagaNodeLoadStatus) nextStatus(eventType SagaNodeEventType) (SagaNodeLoadStatus, error) {
	switch s {
	case LoadNeverStarted:
		if eventType == EventStarted {
			return LoadStarted, nil
		}
	case LoadStarted:
		switch eventType {
		case EventSucceeded:
			return LoadSucceeded, nil
		case EventFailed:
			return LoadFailed, nil
		}
	case LoadSucceeded:
		if eventType == EventUndoStarted {
			return LoadUndoStarted, nil
		}
	case LoadUndoStarted:
		switch eventType {
		case EventUndoFinished:
			return LoadUndoFinished, nil
		case EventUndoFailed:
			return LoadUndoFailed, nil
		}
	}

	// TODO(morganj): Add nodeID to the error context here.
	return LoadNeverStarted, fmt.Errorf(
		"illegal event type %s for current load status %v",
		eventType, s,
	)
}

// SagaLog represents the write log for a saga.
type SagaLog struct {
	sync.Mutex // for thread-safety
	sagaID     SagaID
	unwinding  bool
	events     []*SagaNodeEvent
	nodeStatus map[SagaNodeID]SagaNodeLoadStatus
}

// NewEmptySagaLog creates a new, empty SagaLog.
func NewEmptySagaLog(sagaID SagaID) *SagaLog {
	return &SagaLog{
		sagaID:     sagaID,
		events:     make([]*SagaNodeEvent, 0),
		nodeStatus: make(map[SagaNodeID]SagaNodeLoadStatus),
	}
}

// NewSagaLogRecover creates a new SagaLog from a list of events (for recovery).
func NewSagaLogRecover(sagaID SagaID, events []*SagaNodeEvent) (*SagaLog, error) {
	log := NewEmptySagaLog(sagaID)

	// Sort the events by SagaNodeEventType to ensure a valid replay order.
	sort.Slice(events, func(i, j int) bool {
		return events[i].EventType < events[j].EventType
	})

	for _, event := range events {
		if event.SagaID != sagaID {
			return nil, fmt.Errorf(
				"event in log for different saga (%s) than requested (%s)",
				event.SagaID, sagaID,
			)
		}

		if err := log.Record(event); err != nil {
			return nil, fmt.Errorf(
				"error recovering SagaLog: %w", err,
			)
		}
	}

	return log, nil
}

// Record adds an event to the SagaLog.
func (l *SagaLog) Record(event *SagaNodeEvent) error {
	l.Lock()
	defer l.Unlock()

	currentStatus := l.loadStatusForNode(event.NodeID)
	nextStatus, err := currentStatus.nextStatus(event.EventType)
	if err != nil {
		return err
	}

	switch nextStatus {
	case LoadFailed, LoadUndoStarted, LoadUndoFinished:
		l.unwinding = true
	}

	l.nodeStatus[event.NodeID] = nextStatus
	l.events = append(l.events, event)
	return nil
}

// Unwinding returns true if the SagaLog is currently unwinding.
func (l *SagaLog) Unwinding() bool {
	l.Lock()
	defer l.Unlock()

	return l.unwinding
}

// loadStatusForNode returns the SagaNodeLoadStatus for a given node ID.
func (l *SagaLog) loadStatusForNode(nodeID SagaNodeID) SagaNodeLoadStatus {
	l.Lock()
	defer l.Unlock()

	status, exists := l.nodeStatus[nodeID]
	if !exists {
		return LoadNeverStarted
	}
	return status
}

// Events returns the slice of events in the SagaLog.
func (l *SagaLog) Events() []*SagaNodeEvent {
	l.Lock()
	defer l.Unlock()

	return l.events
}

// SagaLogPretty is a helper for pretty-printing a SagaLog.
type SagaLogPretty struct {
	Log *SagaLog
}

// String implements the fmt.Stringer interface for SagaLogPretty.
func (p *SagaLogPretty) String() string {
	p.Log.Lock() // Lock for reading during pretty-printing
	defer p.Log.Unlock()

	var sb strings.Builder
	sb.WriteString("SAGA LOG:\n")
	sb.WriteString(fmt.Sprintf("saga id:   %s\n", p.Log.sagaID))
	direction := "forward"
	if p.Log.unwinding {
		direction = "unwinding"
	}
	sb.WriteString(fmt.Sprintf("direction: %s\n", direction))
	sb.WriteString(fmt.Sprintf("events (%d total):\n", len(p.Log.events)))
	sb.WriteString("\n")
	for i, event := range p.Log.events {
		sb.WriteString(fmt.Sprintf("%03d %s\n", i+1, event.String()))
	}
	return sb.String()
}

// MarshalJSON implements the json.Marshaler interface for SagaNodeLoadStatus.
func (s SagaNodeLoadStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// UnmarshalJSON implements the json.Unmarshaler interface for SagaNodeLoadStatus.
func (s *SagaNodeLoadStatus) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}

	switch str {
	case "NeverStarted":
		*s = LoadNeverStarted
	case "Started":
		*s = LoadStarted
	case "Succeeded":
		*s = LoadSucceeded
	case "Failed":
		*s = LoadFailed
	case "UndoStarted":
		*s = LoadUndoStarted
	case "UndoFinished":
		*s = LoadUndoFinished
	case "UndoFailed":
		*s = LoadUndoFailed
	default:
		return fmt.Errorf("invalid SagaNodeLoadStatus: %s", str)
	}

	return nil
}

// String returns the string representation of the SagaNodeLoadStatus.
func (s SagaNodeLoadStatus) String() string {
	switch s {
	case LoadNeverStarted:
		return "NeverStarted"
	case LoadStarted:
		return "Started"
	case LoadSucceeded:
		return "Succeeded"
	case LoadFailed:
		return "Failed"
	case LoadUndoStarted:
		return "UndoStarted"
	case LoadUndoFinished:
		return "UndoFinished"
	case LoadUndoFailed:
		return "UndoFailed"
	default:
		return fmt.Sprintf("Unknown SagaNodeLoadStatus: %d", s)
	}
}
