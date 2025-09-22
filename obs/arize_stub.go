package obs

import "context"

type arizeSinkStub struct{}

func newArizeSinkStub() *arizeSinkStub {
	return &arizeSinkStub{}
}

func (arizeSinkStub) LogCompletion(context.Context, Completion) error { return nil }

func (arizeSinkStub) Shutdown(context.Context) error { return nil }
