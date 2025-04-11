// Code generated by mockery v2.53.3. DO NOT EDIT.

package codehosting

import mock "github.com/stretchr/testify/mock"

// MockVcsProviderFactory is an autogenerated mock type for the VcsProviderFactory type
type MockVcsProviderFactory struct {
	mock.Mock
}

type MockVcsProviderFactory_Expecter struct {
	mock *mock.Mock
}

func (_m *MockVcsProviderFactory) EXPECT() *MockVcsProviderFactory_Expecter {
	return &MockVcsProviderFactory_Expecter{mock: &_m.Mock}
}

// Create provides a mock function with given fields: repositoryURL, token
func (_m *MockVcsProviderFactory) Create(repositoryURL string, token string) Platform {
	ret := _m.Called(repositoryURL, token)

	if len(ret) == 0 {
		panic("no return value specified for Create")
	}

	var r0 Platform
	if rf, ok := ret.Get(0).(func(string, string) Platform); ok {
		r0 = rf(repositoryURL, token)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(Platform)
		}
	}

	return r0
}

// MockVcsProviderFactory_Create_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'Create'
type MockVcsProviderFactory_Create_Call struct {
	*mock.Call
}

// Create is a helper method to define mock.On call
//   - repositoryURL string
//   - token string
func (_e *MockVcsProviderFactory_Expecter) Create(repositoryURL interface{}, token interface{}) *MockVcsProviderFactory_Create_Call {
	return &MockVcsProviderFactory_Create_Call{Call: _e.mock.On("Create", repositoryURL, token)}
}

func (_c *MockVcsProviderFactory_Create_Call) Run(run func(repositoryURL string, token string)) *MockVcsProviderFactory_Create_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(string), args[1].(string))
	})
	return _c
}

func (_c *MockVcsProviderFactory_Create_Call) Return(_a0 Platform) *MockVcsProviderFactory_Create_Call {
	_c.Call.Return(_a0)
	return _c
}

func (_c *MockVcsProviderFactory_Create_Call) RunAndReturn(run func(string, string) Platform) *MockVcsProviderFactory_Create_Call {
	_c.Call.Return(run)
	return _c
}

// NewMockVcsProviderFactory creates a new instance of MockVcsProviderFactory. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
// The first argument is typically a *testing.T value.
func NewMockVcsProviderFactory(t interface {
	mock.TestingT
	Cleanup(func())
}) *MockVcsProviderFactory {
	mock := &MockVcsProviderFactory{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
