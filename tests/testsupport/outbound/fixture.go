package outboundfixture

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

const Env = "AGENTEAM_OUTBOUND_FIXTURE"
const Label = "agenteam.d04.networkfixture"

type Descriptor struct{ ContainerID, NetworkID, Nonce, ControlPort, PrivateIP, CAFile string }

func Load() (*Descriptor, error) {
	path := os.Getenv(Env)
	if path == "" {
		return nil, errors.New("owned outbound fixture required")
	}
	st, e := os.Stat(path)
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 {
		return nil, errors.New("invalid outbound fixture descriptor")
	}
	raw, e := os.ReadFile(path)
	if e != nil || len(raw) > 16384 {
		return nil, errors.New("invalid outbound fixture descriptor")
	}
	var d Descriptor
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&d) != nil {
		return nil, errors.New("invalid outbound fixture descriptor")
	}
	if e = d.Verify(); e != nil {
		return nil, e
	}
	return &d, nil
}
func (d *Descriptor) Verify() error {
	nonce, e := hex.DecodeString(d.Nonce)
	ip, ie := netip.ParseAddr(d.PrivateIP)
	port, pe := strconv.Atoi(d.ControlPort)
	if e != nil || len(nonce) != 16 || ie != nil || outbound.Classify(ip) != outbound.Private || pe != nil || port < 1 || port > 65535 || len(d.ContainerID) != 64 || len(d.NetworkID) != 64 {
		return errors.New("invalid outbound fixture identity")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, e := pgfixture.Docker(ctx, "inspect", "--format", "{{json .}}", d.ContainerID)
	if e != nil {
		return e
	}
	var inspected struct {
		ID, Name string
		Config   struct {
			Image  string
			Labels map[string]string
		}
		State           struct{ Running bool }
		NetworkSettings struct {
			Ports    map[string][]struct{ HostIP, HostPort string }
			Networks map[string]struct{ NetworkID, IPAddress string }
		}
	}
	if json.Unmarshal(raw, &inspected) != nil || inspected.ID != d.ContainerID || inspected.Name != "/agenteam-d04-net-"+d.Nonce || inspected.Config.Image != pgfixture.Image || inspected.Config.Labels[Label] != d.Nonce || !inspected.State.Running {
		return errors.New("outbound container ownership mismatch")
	}
	ports := inspected.NetworkSettings.Ports["9000/tcp"]
	if len(ports) != 0 || d.ControlPort != "9000" {
		return errors.New("outbound control port mismatch")
	}
	found := false
	for _, n := range inspected.NetworkSettings.Networks {
		if n.NetworkID == d.NetworkID && n.IPAddress == d.PrivateIP {
			found = true
		}
	}
	if !found {
		return errors.New("outbound private network mismatch")
	}
	raw, e = pgfixture.Docker(ctx, "network", "inspect", "--format", "{{json .Labels}}", d.NetworkID)
	var labels map[string]string
	if e != nil || json.Unmarshal(raw, &labels) != nil || labels[Label] != d.Nonce {
		return errors.New("outbound network ownership mismatch")
	}
	return nil
}

type ScenarioConfig struct {
	Mode, Redirect, Body string
	Size, HeaderBytes    int
	Wire                 *WireScenario
	// Exactly two responses on the same chat URL; the second repeats. Omitted
	// keeps the original fixed Wire behavior. Wire and WireSequence are exclusive.
	WireSequence []WireScenario
}
type WireScenario struct {
	Suffix          string
	Status          int
	Headers         map[string]string
	Chunks          [][]byte
	HoldAfter       *int
	DisconnectAfter *int
}
type Request struct {
	Connection                      int64
	Method, Host, Path, Query, Body string
	Headers                         http.Header
	RequestURI                      string
}
type State struct {
	Requests                          []Request
	Closed                            map[int64]bool
	Connections                       map[int64]string
	ActiveHandlers, CompletedHandlers int
}

func (d *Descriptor) control(ctx context.Context, method, path string, input any, output any) error {
	var body io.Reader
	if input != nil {
		raw, e := json.Marshal(input)
		if e != nil {
			return e
		}
		body = bytes.NewReader(raw)
	}
	req, e := http.NewRequestWithContext(ctx, method, "http://"+net.JoinHostPort(d.PrivateIP, d.ControlPort)+path, body)
	if e != nil {
		return e
	}
	req.Header.Set("X-Fixture-Nonce", d.Nonce)
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	response, e := client.Do(req)
	if e != nil {
		return errors.New("outbound control unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("outbound control rejected")
	}
	if output != nil {
		return json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(output)
	}
	return nil
}
func (d *Descriptor) Ready(ctx context.Context) error {
	return d.control(ctx, "GET", "/ready", nil, nil)
}
func (d *Descriptor) Create(ctx context.Context, cfg ScenarioConfig) (string, error) {
	if e := d.Verify(); e != nil {
		return "", e
	}
	id, e := pgfixture.RandomHex(16)
	if e != nil {
		return "", e
	}
	return id, d.control(ctx, "POST", "/create/"+id, cfg, nil)
}
func (d *Descriptor) Release(ctx context.Context, id string) error {
	return d.control(ctx, "POST", "/release/"+id, nil, nil)
}
func (d *Descriptor) State(ctx context.Context, id string) (State, error) {
	var state State
	e := d.control(ctx, "GET", "/state/"+id, nil, &state)
	return state, e
}
