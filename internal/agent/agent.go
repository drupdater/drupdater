// Package agent calls drupdater's Composer agent: the `composer drupdater:*` commands the image
// installs as a global plugin (agent/ in this repository). Each command reads one JSON request on
// stdin and answers with one JSON envelope on stdout; agent/protocol holds an example of each.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/drupdater/drupdater/internal/php"
)

// Protocol is the envelope version this binary speaks. Bump it, here and in agent/src/Protocol.php,
// whenever a request or result changes shape.
const Protocol = 1

// Kind classifies a failure the agent reported. The agent decides it, from the exception it
// caught, so no caller ever reads Composer's console wording.
type Kind string

const (
	// Unobtainable means something the request needed could not be fetched: the package, or a patch.
	Unobtainable Kind = "unobtainable"
	// InvalidRequest means the agent could not read the request.
	InvalidRequest Kind = "invalid_request"
	// ProtocolMismatch means the agent and this binary disagree on Protocol.
	ProtocolMismatch Kind = "protocol_mismatch"
	// Internal is any other failure inside the agent.
	Internal Kind = "internal"
)

// Error is a failure the agent reported, or a reply that could not be read as one.
type Error struct {
	Command string
	Kind    Kind
	// Reason is a short, stable description of why, fit for a log line or a report.
	Reason string
	// Message is the underlying error's own text, for diagnosis only.
	Message string
}

func (e *Error) Error() string {
	text := fmt.Sprintf("%s: %s", e.Command, e.Kind)
	if e.Reason != "" {
		text += " (" + e.Reason + ")"
	}
	if e.Message != "" {
		text += ": " + e.Message
	}
	return text
}

type request struct {
	Protocol int `json:"protocol"`
	Request  any `json:"request"`
}

type envelope struct {
	Protocol int             `json:"protocol"`
	Command  string          `json:"command"`
	OK       bool            `json:"ok"`
	Result   json.RawMessage `json:"result"`
	Error    *struct {
		Kind    Kind   `json:"kind"`
		Reason  string `json:"reason"`
		Message string `json:"message"`
	} `json:"error"`
}

// Call runs `composer <command>` in c's directory with req as its request, and decodes the
// result into Resp. A failure the agent reported is an *Error.
func Call[Resp any](ctx context.Context, c php.Command, command string, req any) (Resp, error) {
	var result Resp

	body, err := json.Marshal(request{Protocol: Protocol, Request: req})
	if err != nil {
		return result, fmt.Errorf("failed to encode the %s request: %w", command, err)
	}
	c.Stdin = bytes.NewReader(body)

	stdout, stderr, runErr := c.Split(ctx, command, "--no-interaction", "--no-ansi")

	reply, ok := lastEnvelope(stdout, command)
	if !ok {
		// No envelope at all: composer itself failed, or the agent is not installed.
		return result, fmt.Errorf("%s did not answer: %w, output: %s", command, errors.Join(runErr, errNoEnvelope), stderr)
	}
	if reply.Protocol != Protocol {
		return result, &Error{
			Command: command,
			Kind:    ProtocolMismatch,
			Reason:  fmt.Sprintf("agent speaks protocol %d, drupdater speaks %d", reply.Protocol, Protocol),
		}
	}
	if !reply.OK {
		agentErr := &Error{Command: command, Kind: Internal}
		if reply.Error != nil {
			agentErr.Kind, agentErr.Reason, agentErr.Message = reply.Error.Kind, reply.Error.Reason, reply.Error.Message
		}
		return result, agentErr
	}
	if runErr != nil {
		return result, fmt.Errorf("%s answered but exited with %w, output: %s", command, runErr, stderr)
	}
	if err := json.Unmarshal(reply.Result, &result); err != nil {
		return result, fmt.Errorf("failed to decode the %s result: %w", command, err)
	}
	return result, nil
}

var errNoEnvelope = errors.New("no reply envelope on stdout")

// lastEnvelope reads the last line of stdout: another plugin may print to stdout before the agent
// answers, and the agent always answers last.
func lastEnvelope(stdout string, command string) (envelope, bool) {
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	var reply envelope
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &reply); err != nil || reply.Command != command {
		return envelope{}, false
	}
	return reply, true
}
