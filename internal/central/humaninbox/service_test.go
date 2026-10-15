package humaninbox

import (
	"context"
	"errors"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"testing"
)

type unreadStore struct{ Store }

func TestSourceReferenceCannotAuthorizeInboxSourceRead(t *testing.T) {
	store := &unreadStore{}
	authority := &Authority{store: store}
	other := &Authority{store: store}
	p, _ := f.NewID[i.Project]()
	id, _ := f.NewID[struct{}]()
	tx := f.NewTx()
	for name, ctx := range map[string]context.Context{
		"bare":           context.Background(),
		"inactive":       context.WithValue(context.Background(), sourceReadKey{}, &sourceReadWitness{owner: authority, tx: tx, project: p, source: id.String()}),
		"foreign-owner":  context.WithValue(context.Background(), sourceReadKey{}, &sourceReadWitness{owner: other, tx: tx, project: p, source: id.String(), active: true}),
		"foreign-tx":     context.WithValue(context.Background(), sourceReadKey{}, &sourceReadWitness{owner: authority, tx: f.NewTx(), project: p, source: id.String(), active: true}),
		"scan-for-point": context.WithValue(context.Background(), sourceReadKey{}, &sourceReadWitness{owner: authority, tx: tx, project: p, source: id.String(), active: true, scan: true}),
	} {
		t.Run(name, func(t *testing.T) {
			err := authority.RequireSourceReadInTx(ctx, tx, store, p, id.String(), false)
			var failure *f.Fault
			if !errors.As(err, &failure) || failure.Code != f.Forbidden {
				t.Fatal("unissued source access", err)
			}
		})
	}
}
