package statemachine

// Attribution notice:
// Portions of this code adapted from https://github.com/looplab/fsm

// EventName is the name of an event
type EventName string

// State is a state
type State string

// EmptyState means there is no state
const EmptyState State = ""

// Definition defines the a state machine in terms of
// - the inital state
// - a set of transitions defined an event name a source, and a destination
// - a set of callbacks
type Definition interface {
	// Initial returns the initial state of this machine definition
	Initial() State
	// Transition returns whether a transition is possible for a given event
	// and src state, and the destination state if the transition is possible
	Transition(event EventName, src State) (State, bool)

	// LeaveCallbackForState returns the callback for leaving the given state (or nil)
	LeaveCallbackForState(src State) Callback
	// LeaveCallback returns the callback for leaving any state (or nil)
	LeaveCallbackAllStates() Callback
	// EnterCallbackForState returns the callback for entering the given state (or nil)
	EnterCallbackForState(dst State) Callback
	// EnterCallback returns the callback for entering any state (or nil)
	EnterCallbackAllStates() Callback

	// BeforeCallbackForEvent returns the callback to run before the given event (or nil)
	BeforeCallbackForEvent(event EventName) Callback
	// BeforeCallbackForAllEvents returns the callback to run before any event (or nil)
	BeforeCallbackForAllEvents() Callback
	// AfterCallbackForEvent returns the callback to run after the given event (or nil)
	AfterCallbackForEvent(event EventName) Callback
	// AfterCallbackForAllEvents returns the callback to run after any event (or nil)
	AfterCallbackForAllEvents() Callback
}

// StateMachine is an instance of a running finite state machine
type StateMachine interface {
	// Definition returns the definition this instance is based on
	Definition() Definition
	// Current returns the current state
	Current() State
	// Is returns true if state is the current state.
	Is(state State) bool
	// SetState allows the user to manually move to the given state from current state.
	// The call does not trigger any callbacks, if defined.
	SetState(state State)
	// Can returns true if event can occur in the current state.
	Can(event EventName)
	// AvailableTransitions returns a list of event transitions avilable in the
	// current state.
	AvailableTransitions() []EventName
	// Cannot returns true if event can not occure in the current state.
	// It is a convenience method to help code read nicely.
	Cannot(event EventName) bool

	// Event initiates a state transition with the named event.
	//
	// The call takes a variable number of arguments that will be passed to the
	// callback, if defined.
	//
	// It will return nil if the state change is ok or one of these errors:
	// - event X inappropriate in current state Y
	//
	// - event X does not exist
	//
	// - internal error on state transition
	//
	// The last error should never occur in this situation and is a sign of an
	// internal bug.
	Event(event EventName, args ...interface{}) error
}

// EventDesc represents an event when initializing the FSM.
//
// The event can have one or more source states that is valid for performing
// the transition. If the FSM is in one of the source states it will end up in
// the specified destination state, calling all defined callbacks as it goes.
type EventDesc struct {
	// Name is the event name used when calling for a transition.
	Name EventName

	// Src is a slice of source states that the FSM must be in to perform a
	// state transition.
	Src []State

	// Dst is the destination state that the FSM will be in if the transition
	// succeds.
	Dst State
}

// Callback is a function type that callbacks should use. Event is the current
// event info as the callback happens.
type Callback func(*Event)

// Events is a shorthand for defining the transition map in NewFSM.
type Events []EventDesc

// Callbacks is a shorthand for defining the callbacks in NewFSM.a
type Callbacks map[string]Callback

// Event is the info that get passed as a reference in the callbacks.
type Event struct {
	// StateMachine is a reference to the current StateMachine.
	StateMachine StateMachine

	// Event is the event name.
	Event EventName

	// Src is the state before the transition.
	Src State

	// Dst is the state after the transition.
	Dst State

	// Err is an optional error that can be returned from a callback.
	Err error

	// Args is a optinal list of arguments passed to the callback.
	Args []interface{}
}
