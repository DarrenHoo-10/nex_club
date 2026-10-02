package id

import "github.com/google/uuid"

type Generator interface {
	New() uuid.UUID
}

type Random struct{}

func (Random) New() uuid.UUID { return uuid.New() }

type Sequence struct{ n int }

func (s *Sequence) New() uuid.UUID {
	s.n++
	var id uuid.UUID
	id[0] = byte(s.n >> 8)
	id[1] = byte(s.n)
	id[15] = 1
	return id
}
