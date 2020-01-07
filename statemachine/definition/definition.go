package definition

import (
	"strings"

	"github.com/filecoin-project/go-fil-components/statemachine"
)

type definition struct {
	initial     statemachine.State
	transitions map[eKey]statemachine.State
	callbacks   map[cKey]statemachine.Callback
}

// NewDefinition defines an FSM from events and callbacks.
//
// The events and transitions are specified as a slice of Event structs
// specified as Events. Each Event is mapped to one or more internal
// transitions from Event.Src to Event.Dst.
//
// Callbacks are added as a map specified as Callbacks where the key is parsed
// as the callback event as follows, and called in the same order:
//
// 1. before_<EVENT> - called before event named <EVENT>
//
// 2. before_event - called before all events
//
// 3. leave_<OLD_STATE> - called before leaving <OLD_STATE>
//
// 4. leave_state - called before leaving all states
//
// 5. enter_<NEW_STATE> - called after entering <NEW_STATE>
//
// 6. enter_state - called after entering all states
//
// 7. after_<EVENT> - called after event named <EVENT>
//
// 8. after_event - called after all events
//
// There are also two short form versions for the most commonly used callbacks.
// They are simply the name of the event or state:
//
// 1. <NEW_STATE> - called after entering <NEW_STATE>
//
// 2. <EVENT> - called after event named <EVENT>
//
// If both a shorthand version and a full version is specified it is undefined
// which version of the callback will end up in the internal map. This is due
// to the psuedo random nature of Go maps. No checking for multiple keys is
// currently performed.
func NewDefinition(initial statemachine.State, events []statemachine.EventDesc, callbacks map[string]statemachine.Callback) statemachine.Definition {
	d := definition{
		initial:     initial,
		transitions: make(map[eKey]statemachine.State),
		callbacks:   make(map[cKey]statemachine.Callback),
	}

	// Build transition map and store sets of all events and states.
	allEvents := make(map[statemachine.EventName]bool)
	allStates := make(map[statemachine.State]bool)
	for _, e := range events {
		for _, src := range e.Src {
			d.transitions[eKey{e.Name, src}] = e.Dst
			allStates[src] = true
			allStates[e.Dst] = true
		}
		allEvents[e.Name] = true
	}

	// Map all callbacks to events/states.
	for name, fn := range callbacks {
		var target string
		var callbackType int

		switch {
		case strings.HasPrefix(name, "before_"):
			target = strings.TrimPrefix(name, "before_")
			if target == "event" {
				target = ""
				callbackType = callbackBeforeEvent
			} else if _, ok := allEvents[statemachine.EventName(target)]; ok {
				callbackType = callbackBeforeEvent
			}
		case strings.HasPrefix(name, "leave_"):
			target = strings.TrimPrefix(name, "leave_")
			if target == "state" {
				target = ""
				callbackType = callbackLeaveState
			} else if _, ok := allStates[statemachine.State(target)]; ok {
				callbackType = callbackLeaveState
			}
		case strings.HasPrefix(name, "enter_"):
			target = strings.TrimPrefix(name, "enter_")
			if target == "state" {
				target = ""
				callbackType = callbackEnterState
			} else if _, ok := allStates[statemachine.State(target)]; ok {
				callbackType = callbackEnterState
			}
		case strings.HasPrefix(name, "after_"):
			target = strings.TrimPrefix(name, "after_")
			if target == "event" {
				target = ""
				callbackType = callbackAfterEvent
			} else if _, ok := allEvents[statemachine.EventName(target)]; ok {
				callbackType = callbackAfterEvent
			}
		default:
			target = name
			if _, ok := allStates[statemachine.State(target)]; ok {
				callbackType = callbackEnterState
			} else if _, ok := allEvents[statemachine.EventName(target)]; ok {
				callbackType = callbackAfterEvent
			}
		}

		if callbackType != callbackNone {
			d.callbacks[cKey{target, callbackType}] = fn
		}
	}

	return d
}

// Initial returns the initial state of this machine definition
func (d definition) Initial() statemachine.State {
	return d.initial
}

// Transition returns whether a transition is possible for a given event
// and src state, and the destination state if the transition is possible
func (d definition) Transition(event statemachine.EventName, src statemachine.State) (statemachine.State, bool) {
	dst, ok := d.transitions[eKey{event, src}]
	return dst, ok
}

// LeaveCallbackForState returns the callback for leaving the given state (or nil)
func (d definition) LeaveCallbackForState(src statemachine.State) statemachine.Callback {
	return d.callbacks[cKey{string(src), callbackLeaveState}]
}

// LeaveCallback returns the callback for leaving any state (or nil)
func (d definition) LeaveCallbackAllStates() statemachine.Callback {
	return d.callbacks[cKey{"", callbackLeaveState}]
}

// EnterCallbackForState returns the callback for entering the given state (or nil)
func (d definition) EnterCallbackForState(dst statemachine.State) statemachine.Callback {
	return d.callbacks[cKey{string(dst), callbackEnterState}]
}

// EnterCallback returns the callback for entering any state (or nil)
func (d definition) EnterCallbackAllStates() statemachine.Callback {
	return d.callbacks[cKey{"", callbackEnterState}]
}

// BeforeCallbackForEvent returns the callback to run before the given event (or nil)
func (d definition) BeforeCallbackForEvent(event statemachine.EventName) statemachine.Callback {
	return d.callbacks[cKey{string(event), callbackBeforeEvent}]
}

// BeforeCallbackForAllEvents returns the callback to run before any event (or nil)
func (d definition) BeforeCallbackForAllEvents() statemachine.Callback {
	return d.callbacks[cKey{"", callbackBeforeEvent}]
}

// AfterCallbackForEvent returns the callback to run after the given event (or nil)
func (d definition) AfterCallbackForEvent(event statemachine.EventName) statemachine.Callback {
	return d.callbacks[cKey{string(event), callbackAfterEvent}]
}

// AfterCallbackForAllEvents returns the callback to run after any event (or nil)
func (d definition) AfterCallbackForAllEvents() statemachine.Callback {
	return d.callbacks[cKey{"", callbackAfterEvent}]
}

const (
	callbackNone int = iota
	callbackBeforeEvent
	callbackLeaveState
	callbackEnterState
	callbackAfterEvent
)

// cKey is a struct key used for keeping the callbacks mapped to a target.
type cKey struct {
	// target is either the name of a state or an event depending on which
	// callback type the key refers to. It can also be "" for a non-targeted
	// callback like before_event.
	target string

	// callbackType is the situation when the callback will be run.
	callbackType int
}

// eKey is a struct key used for storing the transition map.
type eKey struct {
	// event is the name of the event that the keys refers to.
	event statemachine.EventName

	// src is the source from where the event can transition.
	src statemachine.State
}
