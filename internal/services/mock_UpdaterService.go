// Code generated by mockery v2.53.3. DO NOT EDIT.

package services

import (
	context "context"

	composer "github.com/drupdater/drupdater/pkg/composer"

	internal "github.com/drupdater/drupdater/internal"

	mock "github.com/stretchr/testify/mock"
)

// MockUpdaterService is an autogenerated mock type for the UpdaterService type
type MockUpdaterService struct {
	mock.Mock
}

type MockUpdaterService_Expecter struct {
	mock *mock.Mock
}

func (_m *MockUpdaterService) EXPECT() *MockUpdaterService_Expecter {
	return &MockUpdaterService_Expecter{mock: &_m.Mock}
}

// UpdateDependencies provides a mock function with given fields: ctx, path, packagesToUpdate, worktree, minimalChanges
func (_m *MockUpdaterService) UpdateDependencies(ctx context.Context, path string, packagesToUpdate []string, worktree internal.Worktree, minimalChanges bool) (DependencyUpdateReport, error) {
	ret := _m.Called(ctx, path, packagesToUpdate, worktree, minimalChanges)

	if len(ret) == 0 {
		panic("no return value specified for UpdateDependencies")
	}

	var r0 DependencyUpdateReport
	var r1 error
	if rf, ok := ret.Get(0).(func(context.Context, string, []string, internal.Worktree, bool) (DependencyUpdateReport, error)); ok {
		return rf(ctx, path, packagesToUpdate, worktree, minimalChanges)
	}
	if rf, ok := ret.Get(0).(func(context.Context, string, []string, internal.Worktree, bool) DependencyUpdateReport); ok {
		r0 = rf(ctx, path, packagesToUpdate, worktree, minimalChanges)
	} else {
		r0 = ret.Get(0).(DependencyUpdateReport)
	}

	if rf, ok := ret.Get(1).(func(context.Context, string, []string, internal.Worktree, bool) error); ok {
		r1 = rf(ctx, path, packagesToUpdate, worktree, minimalChanges)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// MockUpdaterService_UpdateDependencies_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'UpdateDependencies'
type MockUpdaterService_UpdateDependencies_Call struct {
	*mock.Call
}

// UpdateDependencies is a helper method to define mock.On call
//   - ctx context.Context
//   - path string
//   - packagesToUpdate []string
//   - worktree internal.Worktree
//   - minimalChanges bool
func (_e *MockUpdaterService_Expecter) UpdateDependencies(ctx interface{}, path interface{}, packagesToUpdate interface{}, worktree interface{}, minimalChanges interface{}) *MockUpdaterService_UpdateDependencies_Call {
	return &MockUpdaterService_UpdateDependencies_Call{Call: _e.mock.On("UpdateDependencies", ctx, path, packagesToUpdate, worktree, minimalChanges)}
}

func (_c *MockUpdaterService_UpdateDependencies_Call) Run(run func(ctx context.Context, path string, packagesToUpdate []string, worktree internal.Worktree, minimalChanges bool)) *MockUpdaterService_UpdateDependencies_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(context.Context), args[1].(string), args[2].([]string), args[3].(internal.Worktree), args[4].(bool))
	})
	return _c
}

func (_c *MockUpdaterService_UpdateDependencies_Call) Return(_a0 DependencyUpdateReport, _a1 error) *MockUpdaterService_UpdateDependencies_Call {
	_c.Call.Return(_a0, _a1)
	return _c
}

func (_c *MockUpdaterService_UpdateDependencies_Call) RunAndReturn(run func(context.Context, string, []string, internal.Worktree, bool) (DependencyUpdateReport, error)) *MockUpdaterService_UpdateDependencies_Call {
	_c.Call.Return(run)
	return _c
}

// UpdateDrupal provides a mock function with given fields: ctx, path, worktree, sites
func (_m *MockUpdaterService) UpdateDrupal(ctx context.Context, path string, worktree internal.Worktree, sites []string) (UpdateHooksPerSite, error) {
	ret := _m.Called(ctx, path, worktree, sites)

	if len(ret) == 0 {
		panic("no return value specified for UpdateDrupal")
	}

	var r0 UpdateHooksPerSite
	var r1 error
	if rf, ok := ret.Get(0).(func(context.Context, string, internal.Worktree, []string) (UpdateHooksPerSite, error)); ok {
		return rf(ctx, path, worktree, sites)
	}
	if rf, ok := ret.Get(0).(func(context.Context, string, internal.Worktree, []string) UpdateHooksPerSite); ok {
		r0 = rf(ctx, path, worktree, sites)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(UpdateHooksPerSite)
		}
	}

	if rf, ok := ret.Get(1).(func(context.Context, string, internal.Worktree, []string) error); ok {
		r1 = rf(ctx, path, worktree, sites)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// MockUpdaterService_UpdateDrupal_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'UpdateDrupal'
type MockUpdaterService_UpdateDrupal_Call struct {
	*mock.Call
}

// UpdateDrupal is a helper method to define mock.On call
//   - ctx context.Context
//   - path string
//   - worktree internal.Worktree
//   - sites []string
func (_e *MockUpdaterService_Expecter) UpdateDrupal(ctx interface{}, path interface{}, worktree interface{}, sites interface{}) *MockUpdaterService_UpdateDrupal_Call {
	return &MockUpdaterService_UpdateDrupal_Call{Call: _e.mock.On("UpdateDrupal", ctx, path, worktree, sites)}
}

func (_c *MockUpdaterService_UpdateDrupal_Call) Run(run func(ctx context.Context, path string, worktree internal.Worktree, sites []string)) *MockUpdaterService_UpdateDrupal_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(context.Context), args[1].(string), args[2].(internal.Worktree), args[3].([]string))
	})
	return _c
}

func (_c *MockUpdaterService_UpdateDrupal_Call) Return(_a0 UpdateHooksPerSite, _a1 error) *MockUpdaterService_UpdateDrupal_Call {
	_c.Call.Return(_a0, _a1)
	return _c
}

func (_c *MockUpdaterService_UpdateDrupal_Call) RunAndReturn(run func(context.Context, string, internal.Worktree, []string) (UpdateHooksPerSite, error)) *MockUpdaterService_UpdateDrupal_Call {
	_c.Call.Return(run)
	return _c
}

// UpdatePatches provides a mock function with given fields: ctx, path, worktree, operations, patches
func (_m *MockUpdaterService) UpdatePatches(ctx context.Context, path string, worktree internal.Worktree, operations []composer.PackageChange, patches map[string]map[string]string) (PatchUpdates, map[string]map[string]string) {
	ret := _m.Called(ctx, path, worktree, operations, patches)

	if len(ret) == 0 {
		panic("no return value specified for UpdatePatches")
	}

	var r0 PatchUpdates
	var r1 map[string]map[string]string
	if rf, ok := ret.Get(0).(func(context.Context, string, internal.Worktree, []composer.PackageChange, map[string]map[string]string) (PatchUpdates, map[string]map[string]string)); ok {
		return rf(ctx, path, worktree, operations, patches)
	}
	if rf, ok := ret.Get(0).(func(context.Context, string, internal.Worktree, []composer.PackageChange, map[string]map[string]string) PatchUpdates); ok {
		r0 = rf(ctx, path, worktree, operations, patches)
	} else {
		r0 = ret.Get(0).(PatchUpdates)
	}

	if rf, ok := ret.Get(1).(func(context.Context, string, internal.Worktree, []composer.PackageChange, map[string]map[string]string) map[string]map[string]string); ok {
		r1 = rf(ctx, path, worktree, operations, patches)
	} else {
		if ret.Get(1) != nil {
			r1 = ret.Get(1).(map[string]map[string]string)
		}
	}

	return r0, r1
}

// MockUpdaterService_UpdatePatches_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'UpdatePatches'
type MockUpdaterService_UpdatePatches_Call struct {
	*mock.Call
}

// UpdatePatches is a helper method to define mock.On call
//   - ctx context.Context
//   - path string
//   - worktree internal.Worktree
//   - operations []composer.PackageChange
//   - patches map[string]map[string]string
func (_e *MockUpdaterService_Expecter) UpdatePatches(ctx interface{}, path interface{}, worktree interface{}, operations interface{}, patches interface{}) *MockUpdaterService_UpdatePatches_Call {
	return &MockUpdaterService_UpdatePatches_Call{Call: _e.mock.On("UpdatePatches", ctx, path, worktree, operations, patches)}
}

func (_c *MockUpdaterService_UpdatePatches_Call) Run(run func(ctx context.Context, path string, worktree internal.Worktree, operations []composer.PackageChange, patches map[string]map[string]string)) *MockUpdaterService_UpdatePatches_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(context.Context), args[1].(string), args[2].(internal.Worktree), args[3].([]composer.PackageChange), args[4].(map[string]map[string]string))
	})
	return _c
}

func (_c *MockUpdaterService_UpdatePatches_Call) Return(_a0 PatchUpdates, _a1 map[string]map[string]string) *MockUpdaterService_UpdatePatches_Call {
	_c.Call.Return(_a0, _a1)
	return _c
}

func (_c *MockUpdaterService_UpdatePatches_Call) RunAndReturn(run func(context.Context, string, internal.Worktree, []composer.PackageChange, map[string]map[string]string) (PatchUpdates, map[string]map[string]string)) *MockUpdaterService_UpdatePatches_Call {
	_c.Call.Return(run)
	return _c
}

// NewMockUpdaterService creates a new instance of MockUpdaterService. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
// The first argument is typically a *testing.T value.
func NewMockUpdaterService(t interface {
	mock.TestingT
	Cleanup(func())
}) *MockUpdaterService {
	mock := &MockUpdaterService{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
