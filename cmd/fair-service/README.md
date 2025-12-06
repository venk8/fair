# FAIR Service

A standalone HTTP service wrapper for the FAIR library, deployable on Kubernetes.

## Features

- **Fairness Tracking**: Exposes `RegisterRequest` and `ReportOutcome` via HTTP APIs.
- **Horizontal Scaling**: Configurable via Kubernetes HPA.
- **Health Checks**: `/healthz` and `/readyz` endpoints.
- **Graceful Shutdown**: Handles SIGINT/SIGTERM.

## API Reference

### Register Request

Check if a request should be throttled.

- **Endpoint**: `POST /register`
- **Body**:
  ```json
  {
    "client_id": "user-123"
  }
  ```
- **Response**:
  ```json
  {
    "should_throttle": false,
    "stats": { ... }
  }
  ```

### Report Outcome

Report the result of a request (only for success or resource failure).

- **Endpoint**: `POST /report`
- **Body**:
  ```json
  {
    "client_id": "user-123",
    "outcome": "success"
  }
  ```
  Outcome can be `"success"` or `"failure"`.

## Running Locally

1. **Build**:
   ```bash
   go build -o fair-service ./cmd/fair-service
   ```
2. **Run**:
   ```bash
   PORT=8080 FAIR_ROTATION_FREQUENCY=1m ./fair-service
   ```

## Deploying to Kubernetes

A Helm chart is provided in `deploy/helm/fair-service`.

### Prerequisites

- Kubernetes cluster
- Helm 3+

### Installation

```bash
helm install fair-service ./deploy/helm/fair-service
```

### Configuration

Customize the deployment using `values.yaml` or `--set` flags.

| Parameter | Description | Default |
|-----------|-------------|---------|
| `autoscaling.minReplicas` | Minimum number of pods | `1` |
| `autoscaling.maxReplicas` | Maximum number of pods | `10` |
| `autoscaling.targetCPUUtilizationPercentage` | Target CPU utilization for HPA | `80` |
| `appConfig.rotationFrequency` | Frequency of hash rotation (Go duration string) | `"1m"` |

Example:

```bash
helm install fair-service ./deploy/helm/fair-service \
  --set autoscaling.minReplicas=3 \
  --set autoscaling.maxReplicas=20 \
  --set appConfig.rotationFrequency=5m
```
