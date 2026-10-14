package main

import "testing"

func TestChatWireSequenceBoundsAndCommonRoute(t *testing.T) {
	const identity = "0123456789abcdef0123456789abcdef"
	for _, count := range []int{1, 2, 5, 16, 17} {
		cfg := config{Mode: "openai_chat_wire", WireSequence: make([]wireScenario, count)}
		for n := range cfg.WireSequence {
			cfg.WireSequence[n] = wireScenario{Status: 200, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{[]byte(`{"choices":[]}`)}}
		}
		if got := validWire(&cfg, identity); got != (count >= 2 && count <= 16) {
			t.Fatalf("response count %d acceptance=%t", count, got)
		}
	}
	cfg := config{Mode: "openai_chat_wire", WireSequence: []wireScenario{
		{Status: 200, Suffix: "/v1/chat/completions"},
		{Status: 200, Suffix: "/v2/chat/completions"},
	}}
	if validWire(&cfg, identity) {
		t.Fatal("sequence accepted different request routes")
	}
}
