// Code generated by mockery v2.53.3. DO NOT EDIT.

package services

import mock "github.com/stretchr/testify/mock"

// MockSettingsService is an autogenerated mock type for the SettingsService type
type MockSettingsService struct {
	mock.Mock
}

type MockSettingsService_Expecter struct {
	mock *mock.Mock
}

func (_m *MockSettingsService) EXPECT() *MockSettingsService_Expecter {
	return &MockSettingsService_Expecter{mock: &_m.Mock}
}

// ConfigureDatabase provides a mock function with given fields: dir, site
func (_m *MockSettingsService) ConfigureDatabase(dir string, site string) error {
	ret := _m.Called(dir, site)

	if len(ret) == 0 {
		panic("no return value specified for ConfigureDatabase")
	}

	var r0 error
	if rf, ok := ret.Get(0).(func(string, string) error); ok {
		r0 = rf(dir, site)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// MockSettingsService_ConfigureDatabase_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'ConfigureDatabase'
type MockSettingsService_ConfigureDatabase_Call struct {
	*mock.Call
}

// ConfigureDatabase is a helper method to define mock.On call
//   - dir string
//   - site string
func (_e *MockSettingsService_Expecter) ConfigureDatabase(dir interface{}, site interface{}) *MockSettingsService_ConfigureDatabase_Call {
	return &MockSettingsService_ConfigureDatabase_Call{Call: _e.mock.On("ConfigureDatabase", dir, site)}
}

func (_c *MockSettingsService_ConfigureDatabase_Call) Run(run func(dir string, site string)) *MockSettingsService_ConfigureDatabase_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(string), args[1].(string))
	})
	return _c
}

func (_c *MockSettingsService_ConfigureDatabase_Call) Return(_a0 error) *MockSettingsService_ConfigureDatabase_Call {
	_c.Call.Return(_a0)
	return _c
}

func (_c *MockSettingsService_ConfigureDatabase_Call) RunAndReturn(run func(string, string) error) *MockSettingsService_ConfigureDatabase_Call {
	_c.Call.Return(run)
	return _c
}

// RemoveProfile provides a mock function with given fields: dir, site
func (_m *MockSettingsService) RemoveProfile(dir string, site string) error {
	ret := _m.Called(dir, site)

	if len(ret) == 0 {
		panic("no return value specified for RemoveProfile")
	}

	var r0 error
	if rf, ok := ret.Get(0).(func(string, string) error); ok {
		r0 = rf(dir, site)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// MockSettingsService_RemoveProfile_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'RemoveProfile'
type MockSettingsService_RemoveProfile_Call struct {
	*mock.Call
}

// RemoveProfile is a helper method to define mock.On call
//   - dir string
//   - site string
func (_e *MockSettingsService_Expecter) RemoveProfile(dir interface{}, site interface{}) *MockSettingsService_RemoveProfile_Call {
	return &MockSettingsService_RemoveProfile_Call{Call: _e.mock.On("RemoveProfile", dir, site)}
}

func (_c *MockSettingsService_RemoveProfile_Call) Run(run func(dir string, site string)) *MockSettingsService_RemoveProfile_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(string), args[1].(string))
	})
	return _c
}

func (_c *MockSettingsService_RemoveProfile_Call) Return(_a0 error) *MockSettingsService_RemoveProfile_Call {
	_c.Call.Return(_a0)
	return _c
}

func (_c *MockSettingsService_RemoveProfile_Call) RunAndReturn(run func(string, string) error) *MockSettingsService_RemoveProfile_Call {
	_c.Call.Return(run)
	return _c
}

// NewMockSettingsService creates a new instance of MockSettingsService. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
// The first argument is typically a *testing.T value.
func NewMockSettingsService(t interface {
	mock.TestingT
	Cleanup(func())
}) *MockSettingsService {
	mock := &MockSettingsService{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
