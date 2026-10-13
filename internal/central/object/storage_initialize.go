package object

import (
	"context"
	"sync/atomic"

	"github.com/minio/minio-go/v7"
)

type bucketCreationKey struct{}
type bucketCreation struct{ sent atomic.Bool }

// Only Service.Initialize calls this after the original identity transaction
// has actually committed. Health checks and transfer-origin checks stay read-only.
func (b *Backend) initializeBucket(ctx context.Context, reserved bool) error {
	s := b.state()
	bucket := s.config.data().bucket
	exists, err := s.client.BucketExists(ctx, bucket)
	if err != nil {
		return unavailable(err)
	}
	if exists {
		return b.checkBucketConfiguration(ctx)
	}
	if !reserved {
		return unavailable(nil)
	}
	// BucketExists maps only NoSuchBucket to false,nil. Authentication, network
	// and unknown responses never enter this branch. Default MakeBucket options
	// create a private bucket without versioning, ObjectLock or lifecycle rules.
	creation := &bucketCreation{}
	createCtx := context.WithValue(ctx, bucketCreationKey{}, creation)
	err = s.client.MakeBucket(createCtx, bucket, minio.MakeBucketOptions{Region: "us-east-1"})
	if err != nil && minio.ToErrorResponse(err).Code != "BucketAlreadyOwnedByYou" {
		return unavailable(err)
	}
	// A concurrent creator must still pass every original safety check. The
	// subsequent store-control check also binds this bucket to the original DB.
	return b.CheckBucket(ctx)
}
