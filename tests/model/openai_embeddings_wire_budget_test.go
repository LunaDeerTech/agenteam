//go:build integration

package model_test

import (
	"context"
	"encoding/json"
	"net/http/httptrace"
	"sync"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
)

func TestModelOpenAIEmbeddingsWireBudget(t *testing.T) {
	v, _, ctx := embeddingWireFixture(t)
	v.allow(t, true)
	t.Run("text_structured_and_embeddings_share_global64_and_project8", func(t *testing.T) {
		b := wire.NewBudget()
		ea := embeddingWireAdapter(t, v, b)
		text, structured := v.newAdapter(t, b), v.newAdapter(t, b)
		hold := 0
		ecfg := wireJSON(embeddingWireReply(t, []int{0}, [][]float64{{1, 2}}, nil))
		ecfg.HoldAfter = &hold
		ekey := embeddingWireScenario(t, v, ecfg)
		ccfg := wireJSON(wireReply("text", "stop", nil, nil))
		ccfg.HoldAfter = &hold
		ckey := v.scenario(t, ccfg)
		scfg := wireJSON(wireReply(`{"v":1}`, "stop", nil, nil))
		scfg.HoldAfter = &hold
		skey := v.scenario(t, scfg)
		var embeddings []*wire.EmbeddingExchange
		var chats []*wire.Exchange
		var counts [3]int
		for projectIndex := 0; projectIndex < 8; projectIndex++ {
			project := newID[id.Project](t)
			for slot := 0; slot < 8; slot++ {
				kind := (projectIndex + slot) % 3
				counts[kind]++
				switch kind {
				case 0:
					r, o := embeddingWireInput(t, v, ekey)
					o.ProjectID, o.Limits.Overall = project, 120*time.Second
					embeddings = append(embeddings, embeddingWireStart(t, ctx, ea, r, o))
				case 1:
					r, o := v.input(t, ckey, wire.JSONResponse)
					o.ProjectID, o.Limits.Overall = project, 120*time.Second
					chats = append(chats, v.start(t, ctx, text, r, o))
				case 2:
					r, o := structuredWireInput(t, v, skey, wire.JSONResponse)
					r.ResponseFormat.Schema = json.RawMessage(structuredScalarSchema)
					o.ProjectID, o.Limits.Overall = project, 120*time.Second
					chats = append(chats, v.start(t, ctx, structured, r, o))
				}
			}
			r, o := embeddingWireInput(t, v, ekey)
			o.ProjectID = project
			if x, err := ea.Start(ctx, r, o); x != nil {
				t.Fatal("mixed project ninth embedding admitted")
			} else {
				wireFault(t, err, f.ResourceBusy)
			}
			cr, co := structuredWireInput(t, v, skey, wire.JSONResponse)
			co.ProjectID = project
			if x, err := structured.Start(ctx, cr, co); x != nil {
				t.Fatal("mixed project ninth structured call admitted")
			} else {
				wireFault(t, err, f.ResourceBusy)
			}
		}
		r, o := embeddingWireInput(t, v, ekey)
		if x, err := ea.Start(ctx, r, o); x != nil {
			t.Fatal("mixed global sixty-fifth embedding admitted")
		} else {
			wireFault(t, err, f.ResourceBusy)
		}
		cr, co := v.input(t, ckey, wire.JSONResponse)
		if x, err := text.Start(ctx, cr, co); x != nil {
			t.Fatal("mixed global sixty-fifth text call admitted")
		} else {
			wireFault(t, err, f.ResourceBusy)
		}
		wireWait(t, "exact mixed native dispatches not observed", func() bool {
			return len(v.state(t, ekey).Requests) == counts[0] && len(v.state(t, ckey).Requests) == counts[1] && len(v.state(t, skey).Requests) == counts[2]
		})
		for _, x := range embeddings {
			if x.Joined() {
				t.Fatal("held embedding body released budget")
			}
		}
		for _, x := range chats {
			if x.Joined() {
				t.Fatal("held chat body released budget")
			}
		}
		b.StopAdmission()
		if x, err := ea.Start(ctx, r, o); x != nil {
			t.Fatal("stopped embedding admitted")
		} else {
			wireFault(t, err, f.ShuttingDown)
		}
		if x, err := text.Start(ctx, cr, co); x != nil {
			t.Fatal("stopped text admitted")
		} else {
			wireFault(t, err, f.ShuttingDown)
		}
		sr, so := structuredWireInput(t, v, skey, wire.JSONResponse)
		if x, err := structured.Start(ctx, sr, so); x != nil {
			t.Fatal("stopped structured call admitted")
		} else {
			wireFault(t, err, f.ShuttingDown)
		}
		expired, cancel := context.WithCancel(ctx)
		cancel()
		if err := b.Drain(expired); err == nil || b.Joined() {
			t.Fatal("cancelled Drain claimed held clients terminal")
		}
		cleanup, finish := context.WithTimeout(ctx, 5*time.Second)
		defer finish()
		if err := b.Force(cleanup); err != nil || !b.Joined() {
			t.Fatal("one mixed Force budget failed to actually join", err)
		}
		for _, x := range embeddings {
			if !x.Joined() {
				t.Fatal("embedding escaped actual cleanup")
			}
		}
		for _, x := range chats {
			if !x.Joined() {
				t.Fatal("chat escaped actual cleanup")
			}
		}
		for index, key := range []string{ekey, ckey, skey} {
			v.settled(t, key)
			if len(v.state(t, key).Requests) != counts[index] {
				t.Fatal("rejected mixed work was queued or retried")
			}
		}
	})
	t.Run("force_cancels_both_protocols_before_waiting_in_one_caller_budget", func(t *testing.T) {
		b := wire.NewBudget()
		ea, chat := embeddingWireAdapter(t, v, b), v.newAdapter(t, b)
		hold := 0
		ecfg := wireJSON(embeddingWireReply(t, []int{0}, [][]float64{{1, 2}}, nil))
		ecfg.HoldAfter = &hold
		ekey := embeddingWireScenario(t, v, ecfg)
		ccfg := wireJSON(wireReply(`{"v":1}`, "stop", nil, nil))
		ccfg.HoldAfter = &hold
		ckey := v.scenario(t, ccfg)
		entered := []chan struct{}{make(chan struct{}), make(chan struct{})}
		returned := []chan struct{}{make(chan struct{}), make(chan struct{})}
		released := make(chan struct{})
		var once sync.Once
		release := func() { once.Do(func() { close(released) }) }
		t.Cleanup(release)
		traced := func(index int) context.Context {
			return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{WroteRequest: func(httptrace.WroteRequestInfo) {
				close(entered[index])
				<-released
				close(returned[index])
			}})
		}
		r, o := embeddingWireInput(t, v, ekey)
		ex := embeddingWireStart(t, traced(0), ea, r, o)
		cr, co := structuredWireInput(t, v, ckey, wire.JSONResponse)
		cr.ResponseFormat.Schema = json.RawMessage(structuredScalarSchema)
		cx := v.start(t, traced(1), chat, cr, co)
		for _, checkpoint := range entered {
			waitSignal(t, checkpoint)
		}
		wireWait(t, "two held writers not dispatched", func() bool {
			return len(v.state(t, ekey).Requests) == 1 && len(v.state(t, ckey).Requests) == 1
		})
		short, cancel := context.WithTimeout(ctx, 60*time.Millisecond)
		started := time.Now()
		err := b.Force(short)
		elapsed := time.Since(started)
		done := short.Err()
		cancel()
		wireModelError(t, err, "timeout", "wire_transport_error", false, false)
		if done != context.DeadlineExceeded || elapsed > 560*time.Millisecond || ex.Joined() || cx.Joined() || b.Joined() {
			t.Fatal("Force deadline manufactured actual terminal")
		}
		wireWait(t, "Force did not cancel both types before waiting", func() bool {
			ed, cd := ex.Observe().Decision, cx.Observe().Decision
			return ed != nil && cd != nil && ed.Reason == ac.Cancelled && cd.Reason == ac.Cancelled && ed.Sent && cd.Sent
		})
		if o.Credential.Use(func([]byte) error { return nil }) != nil || co.Credential.Use(func([]byte) error { return nil }) != nil {
			t.Fatal("Force retired borrowed material before actual writer return")
		}
		release()
		for _, checkpoint := range returned {
			waitSignal(t, checkpoint)
		}
		cleanup, finish := context.WithTimeout(ctx, 5*time.Second)
		defer finish()
		if err := b.Drain(cleanup); err != nil || !b.Joined() || !ex.Joined() || !cx.Joined() {
			t.Fatal("released mixed writers did not actually join", err)
		}
		result, err := ex.Result(ctx)
		embeddingWireZero(t, result)
		wireModelError(t, err, "cancelled", "wire_cancelled", true, false)
		v.settled(t, ekey)
		v.settled(t, ckey)
	})
}
