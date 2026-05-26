package keystore

import "fmt"

type Store interface {
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
}

type ErrNotFound struct {
	Key string
}

func (e *ErrNotFound) Error() string {
	return fmt.Sprintf("keystore: key %q not found", e.Key)
}

func IsNotFound(err error) bool {
	_, ok := err.(*ErrNotFound)
	return ok
}
