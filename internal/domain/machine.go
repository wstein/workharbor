package domain

// can reports whether a transition table lists the move from -> to.
func can[S comparable](table map[S][]S, from, to S) bool {
	for _, next := range table[from] {
		if next == to {
			return true
		}
	}
	return false
}
