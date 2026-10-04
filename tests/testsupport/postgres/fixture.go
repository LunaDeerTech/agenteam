// Package pgfixture verifies a task-owned Docker fixture before any connection.
// It never accepts an external DSN or uses a default database credential.
package pgfixture

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

const Image = "pgvector/pgvector@sha256:99a149d3c84cfb0f32d8da7d72737e4643468787220af2223418730f8e9e9cdc"
const UnsupportedImage = "pgvector/pgvector@sha256:16e62164a405447dca191079a924ee5b8a9dbf04fe53128701ffbea857b37782"
const Label = "agenteam.d03.fixture"
const Env = "AGENTEAM_PG_FIXTURE"
const UnsupportedEnv = "AGENTEAM_PG_UNSUPPORTED_FIXTURE"

type Descriptor struct {
	ContainerID string `json:"container_id"`
	NetworkID   string `json:"network_id"`
	Nonce       string `json:"nonce"`
	Port        string `json:"port"`
	Image       string `json:"image"`
	User        string `json:"user"`
	Password    string `json:"password"`
	CAFile      string `json:"ca_file"`
	WrongCAFile string `json:"wrong_ca_file"`
}
type ContainerInspection struct {
	ID     string `json:"Id"`
	Name   string
	Config struct {
		Image  string
		Labels map[string]string
	}
	State           struct{ Running bool }
	NetworkSettings struct {
		Ports    map[string][]struct{ HostIP, HostPort string }
		Networks map[string]struct{ NetworkID string }
	}
}

func Docker(ctx context.Context, args ...string) ([]byte, error) {
	output, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		return nil, errors.New("owned fixture Docker operation failed")
	}
	return output, nil
}
func RandomHex(n int) (string, error) {
	value := make([]byte, n)
	if _, err := rand.Read(value); err != nil {
		return "", errors.New("fixture entropy unavailable")
	}
	return hex.EncodeToString(value), nil
}
func Load() (*Descriptor, error) {
	return load(os.Getenv(Env))
}
func LoadUnsupported() (*Descriptor, error) { return load(os.Getenv(UnsupportedEnv)) }
func load(path string) (*Descriptor, error) {
	if path == "" {
		return nil, errors.New("integration fixture is required")
	}
	stat, err := os.Stat(path)
	if err != nil || !stat.Mode().IsRegular() || stat.Mode().Perm()&0077 != 0 {
		return nil, errors.New("invalid integration fixture file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("unreadable integration fixture")
	}
	defer file.Close()
	var fixture Descriptor
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&fixture) != nil {
		return nil, errors.New("invalid integration fixture")
	}
	if err := fixture.Verify(); err != nil {
		return nil, err
	}
	return &fixture, nil
}
func (f *Descriptor) Verify() error {
	nonce, err := hex.DecodeString(f.Nonce)
	port, portErr := strconv.Atoi(f.Port)
	if err != nil || len(nonce) != 16 || portErr != nil || port < 1 || port > 65535 || f.Image != Image && f.Image != UnsupportedImage || len(f.ContainerID) != 64 || len(f.NetworkID) != 64 || f.User == "" || f.Password == "" {
		return errors.New("invalid integration fixture identity")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := Docker(ctx, "inspect", "--format", "{{json .}}", f.ContainerID)
	if err != nil {
		return err
	}
	var inspected ContainerInspection
	name := "/agenteam-d03-" + f.Nonce
	if f.Image == UnsupportedImage {
		name += "-pg16"
	}
	if json.Unmarshal(output, &inspected) != nil || inspected.ID != f.ContainerID || inspected.Name != name || inspected.Config.Image != f.Image || inspected.Config.Labels[Label] != f.Nonce || !inspected.State.Running {
		return errors.New("fixture container ownership mismatch")
	}
	ports := inspected.NetworkSettings.Ports["5432/tcp"]
	if len(ports) != 1 || ports[0].HostIP != "127.0.0.1" || ports[0].HostPort != f.Port {
		return errors.New("fixture port ownership mismatch")
	}
	networkFound := false
	for _, network := range inspected.NetworkSettings.Networks {
		if network.NetworkID == f.NetworkID {
			networkFound = true
		}
	}
	if !networkFound {
		return errors.New("fixture network ownership mismatch")
	}
	output, err = Docker(ctx, "network", "inspect", "--format", "{{json .Labels}}", f.NetworkID)
	if err != nil {
		return err
	}
	var labels map[string]string
	if json.Unmarshal(output, &labels) != nil || labels[Label] != f.Nonce {
		return errors.New("fixture network label mismatch")
	}
	return postgres.RejectPGEnvironment(os.Environ())
}
func (f *Descriptor) URL(database string) string {
	u := url.URL{Scheme: "postgresql", Host: net.JoinHostPort("127.0.0.1", f.Port), User: url.UserPassword(f.User, f.Password), Path: "/" + database}
	return u.String()
}
func (f *Descriptor) Config(database string, changes map[string]string) (postgres.Config, error) {
	if !f.ownsDatabase(database) {
		return postgres.Config{}, errors.New("unowned fixture database")
	}
	values := map[string]string{"URL": f.URL(database), "TLS_MODE": "verify-full", "CA_FILE": f.CAFile, "LOCK_TIMEOUT": "1s", "STARTUP_TIMEOUT": "30s"}
	for key, value := range changes {
		values[key] = value
	}
	if values["TLS_MODE"] == "disable" && changes["CA_FILE"] == "" {
		delete(values, "CA_FILE")
	}
	return postgres.LoadConfig(func(name string) (string, bool) {
		value, ok := values[strings.TrimPrefix(name, postgres.ConfigPrefix)]
		return value, ok
	}, nil)
}
func (f *Descriptor) Connect(ctx context.Context, database string) (*pgx.Conn, error) {
	if !f.ownsDatabase(database) {
		return nil, errors.New("unowned fixture database")
	}
	if err := postgres.RejectPGEnvironment(os.Environ()); err != nil {
		return nil, err
	}
	// All values originate in the verified descriptor; credentials and every
	// connection coordinate are explicit. TLS file contents are fixture-owned.
	u, _ := url.Parse(f.URL(database))
	query := url.Values{"sslmode": {"verify-full"}, "sslrootcert": {f.CAFile}, "sslcert": {""}, "sslkey": {""}, "passfile": {""}, "application_name": {"agenteam-fixture"}}
	u.RawQuery = query.Encode()
	conn, err := pgx.Connect(ctx, u.String())
	if err != nil {
		return nil, errors.New("fixture connection failed")
	}
	return conn, nil
}
func (f *Descriptor) ownsDatabase(database string) bool {
	return len(f.Nonce) == 32 && (database == "fixture_control" || strings.HasPrefix(database, "d03_"+f.Nonce[:12]+"_"))
}

type Database struct {
	Fixture *Descriptor
	Name    string
}

func NewDatabase(t *testing.T) *Database {
	t.Helper()
	fixture, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	suffix, err := RandomHex(6)
	if err != nil {
		t.Fatal(err)
	}
	db := &Database{fixture, "d03_" + fixture.Nonce[:12] + "_" + suffix}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	admin, err := fixture.Connect(ctx, "fixture_control")
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{db.Name}.Sanitize()); err != nil {
		t.Fatal("fixture database creation failed")
	}
	t.Cleanup(func() {
		if err := fixture.Verify(); err != nil {
			t.Error(err)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		conn, err := fixture.Connect(ctx, "fixture_control")
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close(ctx)
		if _, err := conn.Exec(ctx, "DROP DATABASE "+pgx.Identifier{db.Name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error("owned database cleanup failed")
		}
	})
	return db
}
func (d *Database) Config(t *testing.T, changes map[string]string) postgres.Config {
	t.Helper()
	cfg, err := d.Fixture.Config(d.Name, changes)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
func (d *Database) Connect(t *testing.T) *pgx.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := d.Fixture.Connect(ctx, d.Name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = conn.Close(ctx)
	})
	return conn
}
func (d *Database) Terminate(ctx context.Context, pid int32) error {
	fail := func(code foundation.Code, phase string, cause error) error {
		// Fault keeps formatting safe while explicit inspection can distinguish
		// the stage, caller cancellation, and a real PostgreSQL query error.
		return foundation.NewFault(code, foundation.NotStarted).WithCause(errors.Join(errors.New("fixture termination "+phase), cause))
	}
	if err := ctx.Err(); err != nil {
		return fail(foundation.DependencyUnavailable, "cancelled", err)
	}
	if d == nil || d.Fixture == nil || !d.Fixture.ownsDatabase(d.Name) || pid <= 0 {
		return fail(foundation.InvalidArgument, "invalid target", nil)
	}
	if err := d.Fixture.Verify(); err != nil {
		return fail(foundation.DependencyUnavailable, "ownership verification failed", err)
	}
	conn, err := d.Fixture.Connect(ctx, "fixture_control")
	if err != nil {
		return fail(foundation.DependencyUnavailable, "connection failed", errors.Join(err, ctx.Err()))
	}
	defer conn.Close(ctx)
	var owned, terminated bool
	// Search the exact PID across databases. An absent PID is already cleaned;
	// a present foreign PID must never execute the termination function.
	err = conn.QueryRow(ctx, `SELECT datname IS NOT DISTINCT FROM $1,
CASE WHEN datname IS NOT DISTINCT FROM $1 THEN pg_catalog.pg_terminate_backend(pid) ELSE false END
FROM pg_catalog.pg_stat_activity WHERE pid=$2`, d.Name, pid).Scan(&owned, &terminated)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fail(foundation.DependencyUnavailable, "query failed", err)
	}
	if !owned {
		return fail(foundation.Forbidden, "foreign database target", nil)
	}
	if !terminated {
		// The backend can exit between pg_stat_activity and the signal attempt.
		// Recheck absence with a fresh statement; false while still alive is not
		// success and never authorizes another signal or a wider PID search.
		err = conn.QueryRow(ctx, `SELECT datname IS NOT DISTINCT FROM $1 FROM pg_catalog.pg_stat_activity WHERE pid=$2`, d.Name, pid).Scan(&owned)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fail(foundation.DependencyUnavailable, "absence query failed", err)
		}
		if !owned {
			return fail(foundation.Forbidden, "foreign database target", nil)
		}
		return fail(foundation.ResourceBusy, "signal declined for live owned backend", nil)
	}
	return nil
}
