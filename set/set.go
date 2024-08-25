package set

type Set[T comparable] struct {
	set map[T]struct{}
}

func (s *Set[T]) Insert(k T) {
	if s.set == nil {
		s.set = make(map[T]struct{})
	}
	s.set[k] = struct{}{}
}

func (s *Set[T]) Contains(k T) bool {
	_, ok := s.set[k]
	return ok
}
