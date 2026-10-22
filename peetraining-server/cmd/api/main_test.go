package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCheckHealthEndpoint(t *testing.T) {
	for _, tc := range []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{name: "healthy", statusCode: http.StatusOK},
		{name: "unhealthy", statusCode: http.StatusServiceUnavailable, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.statusCode)
			}))
			defer server.Close()

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := checkHealthEndpoint(ctx, server.Client(), server.URL)
			if (err != nil) != tc.wantErr {
				t.Fatalf("checkHealthEndpoint() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
