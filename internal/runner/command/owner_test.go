package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCommandRequestAndDefaultRedaction(t *testing.T) {
	good := Request{Program: "/usr/bin/program", Args: []string{"literal ; $(not-shell)", "", "two words"}}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, request := range map[string]Request{
		"relative":       {Program: "program"},
		"root":           {Program: "/"},
		"unclean":        {Program: "/bin/../usr/bin/program"},
		"nul":            {Program: "/bin/p", Args: []string{"a\x00b"}},
		"utf8":           {Program: "/bin/p", Args: []string{string([]byte{0xff})}},
		"argument count": {Program: "/bin/p", Args: make([]string, MaxArguments+1)},
		"argument bytes": {Program: "/bin/p", Args: []string{strings.Repeat("s", MaxArgumentBytes)}},
	} {
		t.Run(name, func(t *testing.T) {
			if !errors.Is(request.Validate(), ErrInvalid) {
				t.Fatal("invalid request accepted")
			}
		})
	}
	secret := "private-command-argument"
	request := Request{Program: "/host/" + secret, Args: []string{secret}}
	data, err := json.Marshal(request)
	if err != nil || strings.Contains(string(data), secret) || strings.Contains(fmt.Sprintf("%+v %#v", request, request), secret) || strings.Contains(request.LogValue().String(), secret) {
		t.Fatal("default request projection leaked")
	}
	result := Result{Started: true, Waited: true, ExitCode: 73}
	data, err = json.Marshal(result)
	if err != nil || string(data) != `"command_result"` || fmt.Sprintf("%+v", result) != "command_result" {
		t.Fatal("result must use explicit projection")
	}
}

type heldNative struct {
	runEntered, runRelease, closeEntered, closeRelease chan struct{}
	runs, closes                                       atomic.Int32
	closeErr                                           error
}

func (n *heldNative) run(ctx context.Context, request Request, sink Sink) (Result, error) {
	n.runs.Add(1)
	close(n.runEntered)
	<-n.runRelease
	return Result{Started: true, Waited: true, ExitCode: 7}, nil
}
func (n *heldNative) close() error {
	n.closes.Add(1)
	close(n.closeEntered)
	<-n.closeRelease
	return n.closeErr
}

func TestCommandOwnerWaitsForOriginalReturnAndClose(t *testing.T) {
	n := &heldNative{runEntered: make(chan struct{}), runRelease: make(chan struct{}), closeEntered: make(chan struct{}), closeRelease: make(chan struct{})}
	o := newOwner(n)
	type outcome struct {
		result Result
		err    error
	}
	runDone := make(chan outcome, 1)
	go func() {
		r, e := o.Run(context.Background(), Request{Program: "/not-spawned"}, nil)
		runDone <- outcome{r, e}
	}()
	<-n.runEntered
	if _, err := o.Run(context.Background(), Request{Program: "/not-spawned"}, nil); !errors.Is(err, ErrBusy) {
		t.Fatal("second Run accepted")
	}
	o.Stop()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(o.Drain(cancelled), context.Canceled) || o.Joined() {
		t.Fatal("held Run was released")
	}
	select {
	case <-n.closeEntered:
		t.Fatal("close began before original Run returned")
	default:
	}
	drainDone := make(chan error, 1)
	go func() { drainDone <- o.Drain(context.Background()) }()
	close(n.runRelease)
	returned := <-runDone
	if !errors.Is(returned.err, context.Canceled) || !returned.result.Started || !returned.result.Waited || returned.result.ExitCode != 7 || !returned.result.Cancelled {
		t.Fatal("Stop erased actual process facts")
	}
	<-n.closeEntered
	if o.Joined() {
		t.Fatal("held Close counted joined")
	}
	select {
	case <-drainDone:
		t.Fatal("Drain returned before Close")
	default:
	}
	second := make(chan error, 1)
	go func() { second <- o.Drain(context.Background()) }()
	close(n.closeRelease)
	if err := <-drainDone; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if !o.Joined() || n.runs.Load() != 1 || n.closes.Load() != 1 {
		t.Fatal("original lifecycle not joined once")
	}
}

func TestCommandOwnerStopBeforeStartAndCloseError(t *testing.T) {
	n := &heldNative{runEntered: make(chan struct{}), runRelease: make(chan struct{}), closeEntered: make(chan struct{}), closeRelease: make(chan struct{}), closeErr: errors.New("private-close-detail")}
	close(n.closeRelease)
	o := newOwner(n)
	o.Stop()
	if _, err := o.Run(context.Background(), Request{Program: "/not-spawned"}, nil); !errors.Is(err, ErrClosed) {
		t.Fatal("stopped owner admitted Run")
	}
	err := o.Drain(context.Background())
	if !errors.Is(err, ErrIO) || strings.Contains(err.Error(), "private") || !o.Joined() || n.runs.Load() != 0 || n.closes.Load() != 1 {
		t.Fatal("actual failed Close was not preserved safely")
	}
	if !errors.Is(o.Drain(context.Background()), ErrIO) || n.closes.Load() != 1 {
		t.Fatal("failed original Close retried")
	}
}
