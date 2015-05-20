package door

import (
	"strings"
)

type State struct {
	Latch LatchState `json:"latch"` // Position of the latch (or bolt): locked (extended), unlocked (retracted) or transitioning
	Door  DoorState  `json:"door"`  // Reed switch readout
}

func (s *State) String() string {
	return "State{Door: " + s.Door.String() + ", Latch: " + s.Latch.String() + "}"
}

func (s *State) IsOpen() bool {
	return s.Door == Open
}

func (s *State) IsClosed() bool {
	return s.Door == Closed
}

func (s *State) IsLocked() bool {
	return s.Latch == Locked
}

func (s *State) IsUnlocked() bool {
	return s.Latch == Unlocked
}

func (s *State) IsInFlight() bool {
	switch s.Latch {
	case Locking:
		fallthrough
	case Unlocking:
		return true
	default:
		return false
	}
	return false
}

func (s *State) Same(other *State) bool {
	return (s.Latch == other.Latch && s.Door == other.Door)
}

type LatchState int

const (
	Locked LatchState = iota
	Unlocked
	Locking
	Unlocking
	Unknown
)

func (l LatchState) String() string {
	switch l {
	case Locked:
		return "Locked"
	case Unlocked:
		return "Unlocked"
	case Locking:
		return "Locking"
	case Unlocking:
		return "Unlocking"
	case Unknown:
		return "Unknown"
	default:
		return "**FIXME**"
	}
}

func (l LatchState) MarshalJSON() ([]byte, error) {
	return []byte(`"` + strings.ToLower(l.String()) + `"`), nil
}

type DoorState int

const (
	Open DoorState = iota
	Closed
	NotChecked
)

func (d DoorState) String() string {
	switch d {
	case Closed:
		return "Closed"
	case Open:
		return "Open"
	case NotChecked:
		return "NotChecked"
	default:
		return "**FIXME**"
	}
}

func (d DoorState) MarshalJSON() ([]byte, error) {
	return []byte((`"` + strings.ToLower(d.String()) + `"`)), nil
}
