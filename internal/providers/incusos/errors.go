package incusos

import "errors"

// ErrNotImplemented marks provider operations that are scaffolded but not implemented.
var ErrNotImplemented = errors.New("incusos provider operation is not implemented yet")

// ErrImageNotFound indicates that no IncusOS catalog entry matched a query.
var ErrImageNotFound = errors.New("incusos image not found")
