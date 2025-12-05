package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/satmihir/fair/pkg/config"
	"github.com/satmihir/fair/pkg/service"
	"github.com/satmihir/fair/pkg/tracker"
	fairhttp "github.com/satmihir/fair/pkg/transport/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestE2E(t *testing.T) {
	// Setup mimics main.go
	trackerConfig := config.DefaultFairnessTrackerConfig()
	trackerConfig.RotationFrequency = 1 * time.Minute

	trkB := tracker.NewFairnessTrackerBuilder()
	trk, err := trkB.BuildWithConfig(trackerConfig)
	require.NoError(t, err)

	svc := service.NewService(trk)
	defer svc.Close()

	handler := fairhttp.NewHandler(svc)
	server := httptest.NewServer(handler)
	defer server.Close()

	client := server.Client()

	// Test Register
	t.Run("Register", func(t *testing.T) {
		reqBody, _ := json.Marshal(map[string]string{"client_id": "test-client"})
		resp, err := client.Post(server.URL+"/register", "application/json", bytes.NewBuffer(reqBody))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)
		assert.Contains(t, result, "should_throttle")
		assert.False(t, result["should_throttle"].(bool))
	})

	// Test Report Success
	t.Run("Report Success", func(t *testing.T) {
		reqBody, _ := json.Marshal(map[string]string{"client_id": "test-client", "outcome": "success"})
		resp, err := client.Post(server.URL+"/report", "application/json", bytes.NewBuffer(reqBody))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	// Test Report Failure
	t.Run("Report Failure", func(t *testing.T) {
		reqBody, _ := json.Marshal(map[string]string{"client_id": "test-client", "outcome": "failure"})
		resp, err := client.Post(server.URL+"/report", "application/json", bytes.NewBuffer(reqBody))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	// Test Report Invalid
	t.Run("Report Invalid", func(t *testing.T) {
		reqBody, _ := json.Marshal(map[string]string{"client_id": "test-client", "outcome": "unknown"})
		resp, err := client.Post(server.URL+"/report", "application/json", bytes.NewBuffer(reqBody))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	// Test Healthz
	t.Run("Healthz", func(t *testing.T) {
		resp, err := client.Get(server.URL+"/healthz")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}
