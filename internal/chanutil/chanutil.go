package chanutil

import "io"

// WithResult is implemented by command structs that carry a result channel.
type WithResult[T any] interface {
	WithResult(c chan any) T
}

// SendReceive sends a command and waits for a typed result.
// Returns io.EOF if the commands channel is closed (actor stopped).
func SendReceive[C WithResult[C], K any](commands chan C, msg C) (result K, err error) {
	defer func() {
		if recover() != nil {
			err = io.EOF
		}
	}()
	re := make(chan any, 1)
	commands <- msg.WithResult(re)
	value := <-re
	switch v := value.(type) {
	case error:
		err = v
	case K:
		result = v
	}
	return
}

// SendReceiveError sends a command and waits for an error-only result.
// Returns io.EOF if the commands channel is closed (actor stopped).
func SendReceiveError[C WithResult[C]](commands chan C, msg C) (err error) {
	defer func() {
		if recover() != nil {
			err = io.EOF
		}
	}()
	re := make(chan any, 1)
	commands <- msg.WithResult(re)
	value := <-re
	if value == nil {
		return nil
	}
	if e, ok := value.(error); ok {
		return e
	}
	return nil
}
