package result

import (
	"reflect"
	"sync"

	"github.com/pkg/errors"
)

type Result[T any] struct {
	mu           sync.Mutex
	Ref          T
	Refs         map[string]T
	Metadata     map[string][]byte
	Attestations map[string][]Attestation[T]
}

func (r *Result[T]) AddMeta(k string, v []byte) {
	r.mu.Lock()
	if r.Metadata == nil {
		r.Metadata = map[string][]byte{}
	}
	r.Metadata[k] = v
	r.mu.Unlock()
}

func (r *Result[T]) AddRef(k string, ref T) {
	r.mu.Lock()
	if r.Refs == nil {
		r.Refs = map[string]T{}
	}
	r.Refs[k] = ref
	r.mu.Unlock()
}

func (r *Result[T]) AddAttestation(k string, v Attestation[T]) {
	r.mu.Lock()
	if r.Attestations == nil {
		r.Attestations = map[string][]Attestation[T]{}
	}
	r.Attestations[k] = append(r.Attestations[k], v)
	r.mu.Unlock()
}

func (r *Result[T]) SetRef(ref T) {
	r.Ref = ref
}

func (r *Result[T]) SingleRef() (T, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.Refs != nil && !reflect.ValueOf(r.Ref).IsValid() {
		var t T
		return t, errors.Errorf("invalid map result")
	}
	return r.Ref, nil
}

func (r *Result[T]) EachRef(fn func(T) error) (err error) {
	if reflect.ValueOf(r.Ref).IsValid() {
		err = fn(r.Ref)
	}
	for _, r := range r.Refs {
		if reflect.ValueOf(r).IsValid() {
			if err1 := fn(r); err1 != nil && err == nil {
				err = err1
			}
		}
	}
	for _, as := range r.Attestations {
		for _, a := range as {
			switch a := a.(type) {
			case *InTotoAttestation[T]:
				if reflect.ValueOf(a.PredicateRef).IsValid() {
					if err1 := fn(a.PredicateRef); err1 != nil && err == nil {
						err = err1
					}
				}
			}
		}
	}
	return err
}

func ConvertResult[U any, V any](r *Result[U], fn func(U) (V, error)) (*Result[V], error) {
	r2 := &Result[V]{}
	var err error

	if reflect.ValueOf(r.Ref).IsValid() {
		r2.Ref, err = fn(r.Ref)
		if err != nil {
			return nil, err
		}
	}

	if r.Refs != nil {
		r2.Refs = map[string]V{}
	}
	for k, r := range r.Refs {
		if reflect.ValueOf(r).IsValid() {
			r2.Refs[k], err = fn(r)
			if err != nil {
				return nil, err
			}
		}
	}

	if r.Attestations != nil {
		r2.Attestations = map[string][]Attestation[V]{}
	}
	for k, as := range r.Attestations {
		for _, a := range as {
			a2, err := ConvertAttestation(a, fn)
			if err != nil {
				return nil, err
			}
			r2.Attestations[k] = append(r2.Attestations[k], a2)
		}
	}

	r2.Metadata = r.Metadata

	return r2, nil
}

func ConvertAttestation[U any, V any](a Attestation[U], fn func(U) (V, error)) (Attestation[V], error) {
	var err error

	switch a := a.(type) {
	case *InTotoAttestation[U]:
		a2 := InTotoAttestation[V]{
			PredicateType: a.PredicateType,
			PredicatePath: a.PredicatePath,
			Subjects:      a.Subjects,
		}
		if reflect.ValueOf(a.PredicateRef).IsValid() {
			a2.PredicateRef, err = fn(a.PredicateRef)
			if err != nil {
				return nil, err
			}
		}
		return &a2, nil
	default:
		return nil, errors.Errorf("unknown attestation type %T", a)
	}
}
