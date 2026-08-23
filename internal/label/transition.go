package label

func Transition(from, to Status) error {
	if !CanTransition(from, to) {
		return ErrInvalidTransition{From: from, To: to}
	}
	return nil
}

type ErrInvalidTransition struct{ From, To Status }

func (e ErrInvalidTransition) Error() string {
	return "invalid status transition: " + string(e.From) + " -> " + string(e.To)
}
