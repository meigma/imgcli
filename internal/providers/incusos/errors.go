package incusos

import "errors"

// ErrImageNotFound indicates that no IncusOS catalog entry matched a query.
var ErrImageNotFound = errors.New("incusos image not found")
