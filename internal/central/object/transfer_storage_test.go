package object

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func TestTransferSigningDurationUsesSigV4Second(t *testing.T) {
	base := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name          string
		now, deadline time.Duration
		want          time.Duration
	}{
		{"first_instant", 0, time.Second, time.Second},
		{"last_nanosecond", time.Second - 1, time.Second, time.Second},
		{"max_duration", 900 * time.Millisecond, 300 * time.Second, 300 * time.Second},
		{"replay_next_second", time.Second + 1, 300 * time.Second, 299 * time.Second},
		{"partial_deadline_floored", 100 * time.Millisecond, 1500 * time.Millisecond, time.Second},
		{"exact_expiry", time.Second, time.Second, 0},
		{"expired", time.Second + 1, time.Second, 0},
		{"no_representable_live_second", 100 * time.Millisecond, 900 * time.Millisecond, 0},
		{"too_long", 900 * time.Millisecond, 301 * time.Second, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := transferSigningDuration(base.Add(test.now), base.Add(test.deadline))
			if got != test.want || (err != nil) != (test.want == 0) {
				t.Fatalf("duration=%s err=%v", got, err)
			}
			if err != nil {
				var fault *foundation.Fault
				if !errors.As(err, &fault) || fault.Code != foundation.InvalidState {
					t.Fatal("unsafe deadline error", err)
				}
			}
		})
	}
}

func TestTransferActualSDKCrossSecondCandidateCannotExtendDeadline(t *testing.T) {
	// Explicit fixed region/credentials make presigning local only. The actual
	// SDK clock still chooses X-Amz-Date; no production clock or SDK is replaced.
	d := storageConfig{endpoint: "127.0.0.1:1", bucket: "signature-unit", accessKey: "unit-access", secretKey: "unit-secret"}
	client, err := minio.New(d.endpoint, &minio.Options{Creds: credentials.NewStaticV4(d.accessKey, d.secretKey, ""), Region: "us-east-1", BucketLookup: minio.BucketLookupPath, MaxRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	now := time.Now()
	before := now.Truncate(time.Second)
	deadline := before.Add(5 * time.Second)
	duration, err := transferSigningDuration(now, deadline)
	if err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(time.Until(before.Add(time.Second + 20*time.Millisecond)))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal("actual signing clock barrier timed out")
	}
	key := "candidate/01900000-0000-7000-8000-000000000001"
	u, err := client.PresignHeader(ctx, http.MethodGet, d.bucket, key, duration, nil, nil)
	if err != nil {
		t.Fatal("local SDK signing failed")
	}
	at, err := time.Parse("20060102T150405Z", u.Query().Get("X-Amz-Date"))
	if err != nil || !at.After(before) || !at.Add(duration).After(deadline) || !time.Now().Before(deadline) {
		t.Fatal("fixture did not cross the actual signing second while live")
	}
	if _, err = validateTransferSignature(u, http.MethodGet, d, key, nil, deadline); err == nil {
		t.Fatal("stale signing duration extended durable deadline")
	}
	duration, err = transferSigningDuration(time.Now(), deadline)
	if err != nil {
		t.Fatal(err)
	}
	u, err = client.PresignHeader(ctx, http.MethodGet, d.bucket, key, duration, nil, nil)
	if err != nil {
		t.Fatal("local SDK resigning failed")
	}
	wire, err := validateTransferSignature(u, http.MethodGet, d, key, nil, deadline)
	if err != nil || wire.Time().After(deadline) {
		t.Fatal("recomputed duration did not preserve the original deadline", err)
	}
	t.Log("actual SDK second changed: stale duration rejected; current duration bounded by original deadline")
}
