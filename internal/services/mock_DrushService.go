// Code generated by mockery v2.51.1. DO NOT EDIT.

package services

import mock "github.com/stretchr/testify/mock"

// MockDrushService is an autogenerated mock type for the DrushService type
type MockDrushService struct {
	mock.Mock
}

type MockDrushService_Expecter struct {
	mock *mock.Mock
}

func (_m *MockDrushService) EXPECT() *MockDrushService_Expecter {
	return &MockDrushService_Expecter{mock: &_m.Mock}
}

// GetUpdateHooks provides a mock function with given fields: dir, site
func (_m *MockDrushService) GetUpdateHooks(dir string, site string) (map[string]UpdateHook, error) {
	ret := _m.Called(dir, site)

	if len(ret) == 0 {
		panic("no return value specified for GetUpdateHooks")
	}

	var r0 map[string]UpdateHook
	var r1 error
	if rf, ok := ret.Get(0).(func(string, string) (map[string]UpdateHook, error)); ok {
		return rf(dir, site)
	}
	if rf, ok := ret.Get(0).(func(string, string) map[string]UpdateHook); ok {
		r0 = rf(dir, site)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(map[string]UpdateHook)
		}
	}

	if rf, ok := ret.Get(1).(func(string, string) error); ok {
		r1 = rf(dir, site)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// MockDrushService_GetUpdateHooks_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'GetUpdateHooks'
type MockDrushService_GetUpdateHooks_Call struct {
	*mock.Call
}

// GetUpdateHooks is a helper method to define mock.On call
//   - dir string
//   - site string
func (_e *MockDrushService_Expecter) GetUpdateHooks(dir interface{}, site interface{}) *MockDrushService_GetUpdateHooks_Call {
	return &MockDrushService_GetUpdateHooks_Call{Call: _e.mock.On("GetUpdateHooks", dir, site)}
}

func (_c *MockDrushService_GetUpdateHooks_Call) Run(run func(dir string, site string)) *MockDrushService_GetUpdateHooks_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(string), args[1].(string))
	})
	return _c
}

func (_c *MockDrushService_GetUpdateHooks_Call) Return(_a0 map[string]UpdateHook, _a1 error) *MockDrushService_GetUpdateHooks_Call {
	_c.Call.Return(_a0, _a1)
	return _c
}

func (_c *MockDrushService_GetUpdateHooks_Call) RunAndReturn(run func(string, string) (map[string]UpdateHook, error)) *MockDrushService_GetUpdateHooks_Call {
	_c.Call.Return(run)
	return _c
}

// NewMockDrushService creates a new instance of MockDrushService. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
// The first argument is typically a *testing.T value.
func NewMockDrushService(t interface {
	mock.TestingT
	Cleanup(func())
}) *MockDrushService {
	mock := &MockDrushService{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
