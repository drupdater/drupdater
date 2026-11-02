package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/drupdater/drupdater/internal/php"
)

// fixture returns one of agent/protocol's examples, compacted to the single line the agent prints.
func fixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "agent", "protocol", name))
	require.NoError(t, err)
	var compact bytes.Buffer
	require.NoError(t, json.Compact(&compact, raw))
	return compact.String()
}

// fakeAgent is a composer that prints stdout, exits with status, and keeps the request it read.
type fakeAgent struct {
	stdout string
	stderr string
	status int

	args    []string
	request string
}

func (f *fakeAgent) command(t *testing.T) php.Command {
	stdinFile := filepath.Join(t.TempDir(), "stdin")
	t.Cleanup(func() {
		body, err := os.ReadFile(stdinFile)
		if err == nil {
			f.request = string(body)
		}
	})
	return php.Command{
		Logger: zap.NewNop(),
		New: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			f.args = append([]string{name}, args...)
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperProcess", "--")
			cmd.Env = []string{
				"GO_WANT_HELPER_PROCESS=1",
				"AGENT_STDOUT=" + f.stdout,
				"AGENT_STDERR=" + f.stderr,
				"AGENT_STATUS=" + strconv.Itoa(f.status),
				"AGENT_STDIN_FILE=" + stdinFile,
				"GOCOVERDIR=/tmp",
			}
			return cmd
		},
	}
}

func (f *fakeAgent) call(t *testing.T) (map[string]any, error) {
	t.Helper()
	return f.callCommand(t, "drupdater:handshake")
}

func (f *fakeAgent) callCommand(t *testing.T, command string) (map[string]any, error) {
	t.Helper()
	return Call[map[string]any](t.Context(), f.command(t), command, struct{}{})
}

func TestHelperProcess(*testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	body, _ := io.ReadAll(os.Stdin)
	_ = os.WriteFile(os.Getenv("AGENT_STDIN_FILE"), body, 0o600) //nolint:gosec // the test names the file
	fmt.Fprint(os.Stdout, os.Getenv("AGENT_STDOUT"))
	fmt.Fprint(os.Stderr, os.Getenv("AGENT_STDERR"))
	status, _ := strconv.Atoi(os.Getenv("AGENT_STATUS"))
	os.Exit(status)
}

func TestCallSendsTheRequestFixture(t *testing.T) {
	f := &fakeAgent{stdout: fixture(t, "handshake/response.json")}

	t.Run("call", func(t *testing.T) {
		_, err := f.call(t)
		require.NoError(t, err)
	})

	assert.JSONEq(t, fixture(t, "handshake/request.json"), f.request)
	assert.Equal(t, []string{"composer", "drupdater:handshake", "--no-interaction", "--no-ansi"}, f.args)
}

func TestCallDecodesTheResult(t *testing.T) {
	f := &fakeAgent{stdout: fixture(t, "project/response.json")}

	result, err := Call[struct {
		WebRoot  string `json:"web_root"`
		LockHash string `json:"lock_hash"`
	}](t.Context(), f.command(t), "drupdater:project", struct{}{})

	require.NoError(t, err)
	assert.Equal(t, "web", result.WebRoot)
	assert.Equal(t, "4b2f0e1c9a7d3e5f6a8b0c1d2e3f4a5b", result.LockHash)
}

func TestCallLogsWhatTheAgentLoggedAtItsLevelOnSuccessAndFailureAlike(t *testing.T) {
	log := `"log":[` +
		`{"level":"debug","message":"d","context":{"addon":"composer_patches","b":2,"a":1}},` +
		`{"level":"info","message":"i","context":{}},` +
		`{"level":"warning","message":"w","context":{}},` +
		`{"level":"error","message":"e","context":{}}]`
	for name, f := range map[string]*fakeAgent{
		"success": {stdout: `{"protocol":3,"command":"drupdater:hook","ok":true,"result":{},` + log + `}`},
		"failure": {stdout: `{"protocol":3,"command":"drupdater:hook","ok":false,"error":{"kind":"failed","reason":"r","message":""},` + log + `}`, status: 1},
	} {
		t.Run(name, func(t *testing.T) {
			core, logs := observer.New(zap.DebugLevel)
			command := f.command(t)
			command.Logger = zap.New(core)

			_, _ = Call[struct{}](t.Context(), command, "drupdater:hook", struct{}{})

			var got []string
			for _, entry := range logs.FilterMessageSnippet("").All() {
				if entry.Message == "d" || entry.Message == "i" || entry.Message == "w" || entry.Message == "e" {
					got = append(got, entry.Level.String()+" "+entry.Message)
				}
			}
			assert.Equal(t, []string{"debug d", "info i", "warn w", "error e"}, got)
			fields := logs.FilterMessage("d").All()[0].Context
			assert.Equal(t, []string{"a", "addon", "b"}, []string{fields[0].Key, fields[1].Key, fields[2].Key}, "fields in name order")
		})
	}
}

func TestCallReadsOnlyTheLastLine(t *testing.T) {
	f := &fakeAgent{stdout: "a plugin's notice\n" + fixture(t, "handshake/response.json") + "\n\n"}

	result, err := f.call(t)

	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestCallReturnsTheAgentsFailure(t *testing.T) {
	f := &fakeAgent{stdout: `{"protocol":3,"command":"drupdater:hook","ok":false,"error":{"kind":"unobtainable",` +
		`"reason":"repository authentication failed",` +
		`"message":"The \"https://packages.example.com/packages.json\" file could not be downloaded (HTTP/2 401 )"}}`, status: 1}

	_, err := f.callCommand(t, "drupdater:hook")

	var agentErr *Error
	require.ErrorAs(t, err, &agentErr)
	assert.Equal(t, &Error{
		Command: "drupdater:hook",
		Kind:    Unobtainable,
		Reason:  "repository authentication failed",
		Message: `The "https://packages.example.com/packages.json" file could not be downloaded (HTTP/2 401 )`,
	}, agentErr)
	assert.Equal(t, `drupdater:hook: unobtainable (repository authentication failed): `+
		`The "https://packages.example.com/packages.json" file could not be downloaded (HTTP/2 401 )`, err.Error())
}

func TestCallReturnsTheAgentsProtocolMismatch(t *testing.T) {
	f := &fakeAgent{stdout: fixture(t, "handshake/protocol-mismatch.json"), status: 1}

	_, err := f.call(t)

	var agentErr *Error
	require.ErrorAs(t, err, &agentErr)
	assert.Equal(t, ProtocolMismatch, agentErr.Kind)
	assert.Equal(t, "drupdater:handshake: protocol_mismatch (drupdater speaks protocol 4, the agent speaks 3)", err.Error())
}

func TestCallRefusesAReplyInAnotherProtocol(t *testing.T) {
	f := &fakeAgent{stdout: `{"protocol":4,"command":"drupdater:handshake","ok":true,"result":{}}`}

	_, err := f.call(t)

	var agentErr *Error
	require.ErrorAs(t, err, &agentErr)
	assert.Equal(t, &Error{Command: "drupdater:handshake", Kind: ProtocolMismatch, Reason: "agent speaks protocol 4, drupdater speaks 3"}, agentErr)
}

func TestCallTreatsAFailureWithoutDetailAsInternal(t *testing.T) {
	f := &fakeAgent{stdout: `{"protocol":3,"command":"drupdater:handshake","ok":false}`, status: 1}

	_, err := f.call(t)

	var agentErr *Error
	require.ErrorAs(t, err, &agentErr)
	assert.Equal(t, &Error{Command: "drupdater:handshake", Kind: Internal}, agentErr)
	assert.Equal(t, "drupdater:handshake: internal", err.Error())
}

func TestCallWithoutAnEnvelopeFails(t *testing.T) {
	for name, f := range map[string]*fakeAgent{
		"composer does not know the command": {stderr: `Command "drupdater:handshake" is not defined.`, status: 1},
		"the output is not an envelope":      {stdout: `{"something":"else"}`},
		"no output at all":                   {},
		"the reply answers another command":  {stdout: fixture(t, "project/response.json")},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.call(t)

			require.ErrorIs(t, err, errNoEnvelope)
			var agentErr *Error
			assert.NotErrorAs(t, err, &agentErr, "nothing answered, so no agent failure to report")
			assert.Contains(t, err.Error(), "drupdater:handshake did not answer")
			assert.Contains(t, err.Error(), f.stderr)
		})
	}
}

func TestCallDistrustsASuccessThatExitedNonZero(t *testing.T) {
	f := &fakeAgent{stdout: fixture(t, "handshake/response.json"), stderr: "segfault", status: 3}

	_, err := f.call(t)

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Contains(t, err.Error(), "answered but exited")
	assert.Contains(t, err.Error(), "segfault")
}

func TestCallRejectsAResultOfTheWrongShape(t *testing.T) {
	f := &fakeAgent{stdout: `{"protocol":3,"command":"drupdater:handshake","ok":true,"result":[1]}`}

	_, err := f.call(t)

	var typeErr *json.UnmarshalTypeError
	require.ErrorAs(t, err, &typeErr)
	assert.Contains(t, err.Error(), "failed to decode the drupdater:handshake result")
}

func TestCallRejectsAnUnencodableRequest(t *testing.T) {
	f := &fakeAgent{}

	_, err := Call[map[string]any](t.Context(), f.command(t), "drupdater:handshake", func() {})

	var typeErr *json.UnsupportedTypeError
	require.ErrorAs(t, err, &typeErr)
	assert.Contains(t, err.Error(), "failed to encode the drupdater:handshake request")
	assert.Nil(t, f.args, "nothing is run for a request that cannot be sent")
}

func TestCallKeepsComposersExplanationOfAFailure(t *testing.T) {
	f := &fakeAgent{
		stdout: `{"protocol":3,"command":"drupdater:resolve","ok":false,"error":{"kind":"failed","reason":"composer update failed","message":"exit code 2"}}`,
		stderr: "Your requirements could not be resolved to an installable set of packages.",
		status: 1,
	}

	_, err := f.callCommand(t, "drupdater:resolve")

	var agentErr *Error
	require.ErrorAs(t, err, &agentErr)
	assert.Equal(t, Failed, agentErr.Kind)
	assert.Equal(t, "Your requirements could not be resolved to an installable set of packages.", agentErr.Output)
	assert.Equal(t, "drupdater:resolve: failed (composer update failed): exit code 2, "+
		"output: Your requirements could not be resolved to an installable set of packages.", err.Error())
}
