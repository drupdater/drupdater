// Code generated by mockery v2.53.3. DO NOT EDIT.

package services

import mock "github.com/stretchr/testify/mock"

// MockInstallerService is an autogenerated mock type for the InstallerService type
type MockInstallerService struct {
	mock.Mock
}

type MockInstallerService_Expecter struct {
	mock *mock.Mock
}

func (_m *MockInstallerService) EXPECT() *MockInstallerService_Expecter {
	return &MockInstallerService_Expecter{mock: &_m.Mock}
}

// InstallDrupal provides a mock function with given fields: repositoryURL, branch, token, sites
func (_m *MockInstallerService) InstallDrupal(repositoryURL string, branch string, token string, sites []string) error {
	ret := _m.Called(repositoryURL, branch, token, sites)

	if len(ret) == 0 {
		panic("no return value specified for InstallDrupal")
	}

	var r0 error
	if rf, ok := ret.Get(0).(func(string, string, string, []string) error); ok {
		r0 = rf(repositoryURL, branch, token, sites)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// MockInstallerService_InstallDrupal_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'InstallDrupal'
type MockInstallerService_InstallDrupal_Call struct {
	*mock.Call
}

// InstallDrupal is a helper method to define mock.On call
//   - repositoryURL string
//   - branch string
//   - token string
//   - sites []string
func (_e *MockInstallerService_Expecter) InstallDrupal(repositoryURL interface{}, branch interface{}, token interface{}, sites interface{}) *MockInstallerService_InstallDrupal_Call {
	return &MockInstallerService_InstallDrupal_Call{Call: _e.mock.On("InstallDrupal", repositoryURL, branch, token, sites)}
}

func (_c *MockInstallerService_InstallDrupal_Call) Run(run func(repositoryURL string, branch string, token string, sites []string)) *MockInstallerService_InstallDrupal_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(string), args[1].(string), args[2].(string), args[3].([]string))
	})
	return _c
}

func (_c *MockInstallerService_InstallDrupal_Call) Return(_a0 error) *MockInstallerService_InstallDrupal_Call {
	_c.Call.Return(_a0)
	return _c
}

func (_c *MockInstallerService_InstallDrupal_Call) RunAndReturn(run func(string, string, string, []string) error) *MockInstallerService_InstallDrupal_Call {
	_c.Call.Return(run)
	return _c
}

// NewMockInstallerService creates a new instance of MockInstallerService. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
// The first argument is typically a *testing.T value.
func NewMockInstallerService(t interface {
	mock.TestingT
	Cleanup(func())
}) *MockInstallerService {
	mock := &MockInstallerService{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
