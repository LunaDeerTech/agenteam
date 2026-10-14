package adapter

import (
	"testing"
	"time"
)

// Exercise the real request/profile boundary, without a model or network.
// The overall attempt window remains owned by the caller.
func TestJSONToolsResponseHeaderBudget(t *testing.T) {
	for _, sample := range []struct {
		name             string
		overall, headers time.Duration
		invalid          bool
	}{
		{name: "short_attempt", overall: time.Second},
		{name: "sixty_second_attempt", overall: 60 * time.Second},
		{name: "long_attempt_header_ceiling", overall: 120 * time.Second},
		{name: "explicit_shorter", overall: 60 * time.Second, headers: 5 * time.Second},
		{name: "explicit_sixty", overall: 60 * time.Second, headers: 60 * time.Second},
		{name: "over_attempt", overall: 30 * time.Second, headers: 31 * time.Second, invalid: true},
		{name: "over_header_ceiling", overall: 120 * time.Second, headers: 61 * time.Second, invalid: true},
		{name: "negative_header", overall: 60 * time.Second, headers: -time.Second, invalid: true},
	} {
		t.Run(sample.name, func(t *testing.T) {
			request, options := jsonToolsInput(t)
			options.Limits.Overall = sample.overall
			options.Limits.ResponseHeaders = sample.headers
			prepared, _, err := prepare(request, options)
			if sample.invalid {
				if err == nil || prepared != nil {
					t.Fatal("invalid stage budget admitted an HTTP request")
				}
				return
			}
			if err != nil || prepared == nil {
				t.Fatalf("valid original attempt budget rejected: %v", err)
			}
			if err := prepared.Body.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
