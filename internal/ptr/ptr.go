// Package ptr holds small pointer helpers shared across the library.
package ptr

// Deref returns the value a pointer points to, or the zero value of its type
// when the pointer is nil.
func Deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}
