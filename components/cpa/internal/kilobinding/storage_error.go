package kilobinding

import (
	"errors"
	"fmt"
	"syscall"
)

// StorageError keeps filesystem details out of the public error string.
type StorageError struct {
	Operation string
	cause     error
}

func (e *StorageError) Error() string        { return ErrStorage.Error() }
func (e *StorageError) Unwrap() error        { return e.cause }
func (e *StorageError) Is(target error) bool { return target == ErrStorage }
func storageError(operation string, cause error) error {
	return &StorageError{Operation: operation, cause: cause}
}

// StorageDiagnostic returns only operation, type and numeric system code.
func StorageDiagnostic(err error) (operation, errorType string, systemCode uint64, ok bool) {
	var failure *StorageError
	if !errors.As(err, &failure) {
		return "", "", 0, false
	}
	var code syscall.Errno
	if errors.As(failure.cause, &code) {
		systemCode = uint64(code)
	}
	return failure.Operation, fmt.Sprintf("%T", failure.cause), systemCode, true
}
