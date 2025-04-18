// Code generated by mockery v2.53.3. DO NOT EDIT.

package drupalorg

import mock "github.com/stretchr/testify/mock"

// MockClient is an autogenerated mock type for the Client type
type MockClient struct {
	mock.Mock
}

type MockClient_Expecter struct {
	mock *mock.Mock
}

func (_m *MockClient) EXPECT() *MockClient_Expecter {
	return &MockClient_Expecter{mock: &_m.Mock}
}

// FindIssueNumber provides a mock function with given fields: text
func (_m *MockClient) FindIssueNumber(text string) (string, bool) {
	ret := _m.Called(text)

	if len(ret) == 0 {
		panic("no return value specified for FindIssueNumber")
	}

	var r0 string
	var r1 bool
	if rf, ok := ret.Get(0).(func(string) (string, bool)); ok {
		return rf(text)
	}
	if rf, ok := ret.Get(0).(func(string) string); ok {
		r0 = rf(text)
	} else {
		r0 = ret.Get(0).(string)
	}

	if rf, ok := ret.Get(1).(func(string) bool); ok {
		r1 = rf(text)
	} else {
		r1 = ret.Get(1).(bool)
	}

	return r0, r1
}

// MockClient_FindIssueNumber_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'FindIssueNumber'
type MockClient_FindIssueNumber_Call struct {
	*mock.Call
}

// FindIssueNumber is a helper method to define mock.On call
//   - text string
func (_e *MockClient_Expecter) FindIssueNumber(text interface{}) *MockClient_FindIssueNumber_Call {
	return &MockClient_FindIssueNumber_Call{Call: _e.mock.On("FindIssueNumber", text)}
}

func (_c *MockClient_FindIssueNumber_Call) Run(run func(text string)) *MockClient_FindIssueNumber_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(string))
	})
	return _c
}

func (_c *MockClient_FindIssueNumber_Call) Return(_a0 string, _a1 bool) *MockClient_FindIssueNumber_Call {
	_c.Call.Return(_a0, _a1)
	return _c
}

func (_c *MockClient_FindIssueNumber_Call) RunAndReturn(run func(string) (string, bool)) *MockClient_FindIssueNumber_Call {
	_c.Call.Return(run)
	return _c
}

// GetIssue provides a mock function with given fields: issueID
func (_m *MockClient) GetIssue(issueID string) (*Issue, error) {
	ret := _m.Called(issueID)

	if len(ret) == 0 {
		panic("no return value specified for GetIssue")
	}

	var r0 *Issue
	var r1 error
	if rf, ok := ret.Get(0).(func(string) (*Issue, error)); ok {
		return rf(issueID)
	}
	if rf, ok := ret.Get(0).(func(string) *Issue); ok {
		r0 = rf(issueID)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*Issue)
		}
	}

	if rf, ok := ret.Get(1).(func(string) error); ok {
		r1 = rf(issueID)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// MockClient_GetIssue_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'GetIssue'
type MockClient_GetIssue_Call struct {
	*mock.Call
}

// GetIssue is a helper method to define mock.On call
//   - issueID string
func (_e *MockClient_Expecter) GetIssue(issueID interface{}) *MockClient_GetIssue_Call {
	return &MockClient_GetIssue_Call{Call: _e.mock.On("GetIssue", issueID)}
}

func (_c *MockClient_GetIssue_Call) Run(run func(issueID string)) *MockClient_GetIssue_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(string))
	})
	return _c
}

func (_c *MockClient_GetIssue_Call) Return(_a0 *Issue, _a1 error) *MockClient_GetIssue_Call {
	_c.Call.Return(_a0, _a1)
	return _c
}

func (_c *MockClient_GetIssue_Call) RunAndReturn(run func(string) (*Issue, error)) *MockClient_GetIssue_Call {
	_c.Call.Return(run)
	return _c
}

// NewMockClient creates a new instance of MockClient. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
// The first argument is typically a *testing.T value.
func NewMockClient(t interface {
	mock.TestingT
	Cleanup(func())
}) *MockClient {
	mock := &MockClient{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
