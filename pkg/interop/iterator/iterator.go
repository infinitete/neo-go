package iterator

// Package iterator provides function signatures that can be used inside
// smart contracts that are written in the neo-go framework.

// Iterator stubs a NEO iterator object type.
type Iterator struct{}

// Create creates an iterator from the given items.
func Create(items []interface{}) Iterator {
	return Iterator{}
}

// Key returns the iterator key.
// TODO: Better description for this.
func Key(it Iterator) interface{} {
	return nil
}

// Keys returns the iterator keys.
func Keys(it Iterator) []interface{} {
	return nil
}

// Values returns the iterator values.
func Values(it Iterator) []interface{} {
	return nil
}
