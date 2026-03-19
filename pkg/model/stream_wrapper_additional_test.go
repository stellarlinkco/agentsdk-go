package model

import (
	"context"
	"errors"
	"testing"
)

type stubModelProvider struct {
	mdl Model
	err error
}

func (s stubModelProvider) Model(context.Context) (Model, error) { return s.mdl, s.err }

type stubModel struct{}

func (s *stubModel) Complete(context.Context, Request) (*Response, error) { return &Response{}, nil }
func (s *stubModel) CompleteStream(context.Context, Request, StreamHandler) error {
	return nil
}

func TestStreamOnlyModelCompleteStream_NilInner(t *testing.T) {
	wrapper := &StreamOnlyModel{}
	err := wrapper.CompleteStream(context.Background(), Request{}, func(StreamResult) error { return nil })
	if err == nil {
		t.Fatalf("expected error for nil inner model")
	}
}

func TestStreamOnlyProviderModel_NilInner(t *testing.T) {
	p := &StreamOnlyProvider{}
	if _, err := p.Model(context.Background()); err == nil {
		t.Fatalf("expected error for nil inner provider")
	}
}

func TestStreamOnlyProviderModel_InnerError(t *testing.T) {
	p := &StreamOnlyProvider{Inner: stubModelProvider{err: errors.New("boom")}}
	if _, err := p.Model(context.Background()); err == nil || err.Error() != "boom" {
		t.Fatalf("expected inner error, got %v", err)
	}
}

func TestStreamOnlyProviderModel_AvoidsDoubleWrap(t *testing.T) {
	inner := &StreamOnlyModel{Inner: &stubModel{}}
	p := &StreamOnlyProvider{Inner: stubModelProvider{mdl: inner}}
	got, err := p.Model(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != inner {
		t.Fatalf("expected model to be returned as-is when already wrapped")
	}
}

func TestStreamOnlyProviderModel_WrapsInner(t *testing.T) {
	inner := &stubModel{}
	p := &StreamOnlyProvider{Inner: stubModelProvider{mdl: inner}}
	got, err := p.Model(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wrapped, ok := got.(*StreamOnlyModel)
	if !ok {
		t.Fatalf("expected StreamOnlyModel, got %T", got)
	}
	if wrapped.Inner != inner {
		t.Fatalf("expected wrapper to reference inner model")
	}
}

func TestStreamOnlyModelCompleteStream_DelegatesToInner(t *testing.T) {
	inner := &fakeStreamInner{}
	wrapper := NewStreamOnlyModel(inner)

	called := false
	err := wrapper.CompleteStream(context.Background(), Request{}, func(StreamResult) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !inner.called {
		t.Fatalf("expected inner CompleteStream to be called")
	}
	if !called {
		t.Fatalf("expected callback to be invoked")
	}
}

type fakeStreamInner struct {
	called bool
}

func (f *fakeStreamInner) Complete(context.Context, Request) (*Response, error) {
	return &Response{}, nil
}
func (f *fakeStreamInner) CompleteStream(_ context.Context, _ Request, cb StreamHandler) error {
	f.called = true
	if cb != nil {
		return cb(StreamResult{Delta: "x"})
	}
	return nil
}
