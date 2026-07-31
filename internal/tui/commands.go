package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vinit-chauhan/es-tool/internal/esclient"
	"github.com/vinit-chauhan/es-tool/internal/util"
)

type requestMsg struct {
	operation string
	epoch     int
	status    int
	body      any
	err       error
}

type healthMsg requestMsg

// requestCmd stamps the response with the connection epoch that was active
// when the request started, so answers from a previously connected cluster
// can be recognized and dropped after a switch.
func requestCmd(
	client *esclient.Client,
	epoch int,
	operation string,
	method string,
	path string,
	body any,
	params map[string]string,
) tea.Cmd {
	return func() tea.Msg {
		status, response, err := client.Request(method, path, body, params)
		return requestMsg{
			operation: operation,
			epoch:     epoch,
			status:    status,
			body:      response,
			err:       requestError(status, response, err),
		}
	}
}

func healthCmd(client *esclient.Client, epoch int) tea.Cmd {
	return func() tea.Msg {
		status, body, err := client.Request("GET", "/_cluster/health", nil, nil)
		return healthMsg{operation: "health", epoch: epoch, status: status, body: body, err: err}
	}
}

func requestError(status int, body any, err error) error {
	if err != nil {
		return err
	}
	if status >= 200 && status < 300 {
		return nil
	}
	if body == nil {
		return fmt.Errorf("HTTP %d", status)
	}
	return fmt.Errorf("HTTP %d: %s", status, util.Dump(body))
}

type healthStatusState int

const (
	stateHealthChecking healthStatusState = iota
	stateHealthGreen
	stateHealthYellow
	stateHealthRed
	stateHealthConnected
	stateHealthAuthError
	stateHealthUnavailable
	stateHealthOffline
)

type healthStatus struct {
	state  healthStatusState
	code   int
	detail string
}

func (h healthStatus) label() string {
	switch h.state {
	case stateHealthGreen:
		return "GREEN"
	case stateHealthYellow:
		return "YELLOW"
	case stateHealthRed:
		return "RED"
	case stateHealthConnected:
		return "CONNECTED"
	case stateHealthAuthError:
		return fmt.Sprintf("AUTH %d", h.code)
	case stateHealthUnavailable:
		return fmt.Sprintf("HTTP %d", h.code)
	case stateHealthOffline:
		return "OFFLINE"
	default:
		return "CHECKING"
	}
}

func healthFromResponse(status int, body any, err error) healthStatus {
	if err != nil {
		return healthStatus{state: stateHealthOffline, detail: err.Error()}
	}
	if status == 401 {
		return healthStatus{state: stateHealthAuthError, code: status}
	}
	if status < 200 || status >= 300 {
		return healthStatus{state: stateHealthUnavailable, code: status}
	}
	if response, ok := body.(map[string]any); ok {
		switch util.AsStr(response["status"]) {
		case "green":
			return healthStatus{state: stateHealthGreen}
		case "yellow":
			return healthStatus{state: stateHealthYellow}
		case "red":
			return healthStatus{state: stateHealthRed}
		}
	}
	return healthStatus{state: stateHealthConnected}
}
