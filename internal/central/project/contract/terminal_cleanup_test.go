package contract

import (
	"testing"

	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func terminalRequestFixture(t *testing.T) ProjectTerminalCleanupRequest {
	t.Helper()
	p := testID[i.Project](1)
	cause := LifecycleCause{OperationID: testID[Operation](2), Action: Delete, ProjectVersion: 3}
	registration, err := i.RegisterService(i.ProjectLifecycle)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := i.InProject(p)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := registration.Actor(cause.OperationID.String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	req := ProjectTerminalCleanupRequest{Actor: actor, Cause: cause, ProjectID: p, Owner: ProjectTerminalOwnerIdentity{Kind: ProjectTerminalAgentOwner, ProcessID: testID[struct{}](3).String(), InstanceID: testID[struct{}](4).String()}}
	if err = req.Validate(); err != nil {
		t.Fatal("invalid complete fixture", err)
	}
	return req
}

func TestProjectTerminalCleanupRequest(t *testing.T) {
	for _, mode := range []string{"apply", "confirm-retired", "confirm-unapplied", "unapplied-without-confirmation", "human", "archive", "wrong-project", "wrong-cause", "unknown-owner", "bad-process", "bad-instance"} {
		t.Run(mode, func(t *testing.T) {
			r := terminalRequestFixture(t)
			valid := false
			switch mode {
			case "apply":
				valid = true
			case "confirm-retired":
				r.Confirmation, valid = true, true
			case "confirm-unapplied":
				r.Confirmation, r.NotApplied, valid = true, true, true
			case "unapplied-without-confirmation":
				r.NotApplied = true
			case "human":
				r.Actor = testHuman(5, 6)
			case "archive":
				r.Cause.Action = Archive
			case "wrong-project":
				r.ProjectID = testID[i.Project](8)
			case "wrong-cause":
				r.Cause.OperationID = testID[Operation](9)
			case "unknown-owner":
				r.Owner.Kind = "other"
			case "bad-process":
				r.Owner.ProcessID = "not-a-process"
			case "bad-instance":
				r.Owner.InstanceID = "not-an-instance"
			}
			if (r.Validate() == nil) != valid || (len(r.RequiredLocks()) == 1) != valid {
				t.Fatal("wrong shape or held-lock requirements")
			}
		})
	}
}
