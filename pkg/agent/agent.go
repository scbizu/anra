package agent

import (
	"errors"
	"sync"

	"github.com/scbizu/tape-go/pkg/tape/storage/bbolt"
)

type Anra struct {
	mu        sync.Mutex
	principal string
	tape      *bbolt.Bbolt
	runtime   runtimeConfig
}

// New requires an initialized bbolt tape. The caller closes the tape.
func New(principal string, tape *bbolt.Bbolt, opts ...Option) (*Anra, error) {
	if principal == "" {
		return nil, errors.New("agent: empty principal")
	}
	if tape == nil {
		return nil, errors.New("agent: nil tape")
	}
	anra := &Anra{
		principal: principal,
		tape:      tape,
		runtime: runtimeConfig{
			maxTurns: defaultMaxTurns,
		},
	}
	for _, opt := range opts {
		if opt == nil {
			return nil, errors.New("agent: nil option")
		}
		if err := opt(anra); err != nil {
			return nil, err
		}
	}
	if err := anra.initialize(); err != nil {
		return nil, err
	}
	return anra, nil
}
