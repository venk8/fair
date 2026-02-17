# Detailed Implementation Plan: State Service for Distributed FAIR (Refined)

This document outlines a super-detailed plan for implementing the State Service as described in `designs/storage_design.md`. This plan has been refined to align with the project's existing structure and conventions.

## Phase 0: Setup and Dependencies

### 0. Add Initial Dependencies
**Goal**: Add the necessary gRPC and Protobuf libraries to the project.
*   **Task 0.1**: Add gRPC and protoc-gen-go-grpc dependencies.
    *   **Context**: The project needs `google.golang.org/grpc` for the server/client and generator tools for compiling the protobuf service definition.
    *   **Commands**:
        ```sh
        go get google.golang.org/grpc
        go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34
        go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.4
        ```
    *   **Verification**: Ensure `go.mod` and `go.sum` are updated.

## Phase 1: Core Service Definition and Data Management

### 1. Define Protobuf Schema (`state_v1.proto`)

**Goal**: Establish the communication contract for the State Service.

*   **Task 1.1**: Create `pkg/state/api/v1/state_v1.proto`.
    *   **Context**: To keep state management code self-contained, the API definition will live in a new `pkg/state/api/` directory.
    *   **Details**:
        *   Define `syntax = "proto3";`
        *   `package fair.state.v1;`
        *   `option go_package = "github.com/satmihir/fair/pkg/state/api/v1";`
        *   Declare `service StateService` with `rpc Sync(stream SyncRequest) returns (stream SyncResponse);`.
        *   Define all required messages: `SyncRequest` (with `oneof`), `StateRequest`, `DeltaUpdate`, `BucketDelta`, `SyncResponse`, and `Bucket`, exactly as specified in the design document.
*   **Task 1.2**: Update `Makefile` to generate Go code from `state_v1.proto`.
    *   **Context**: The existing `make proto` command must be updated to find and compile the new proto file.
    *   **Details**: Modify the `proto` target in the `Makefile` to compile all `.proto` files it finds.
        ```makefile
        # Makefile (updated proto target)
        proto:
        	@echo "Generating Protocol Buffer code..."
        	@protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative `find pkg -name *.proto`
        	@echo "✅ Proto generation completed"
        ```
*   **Task 1.3**: Generate the Go code.
    *   **Command**: `make proto`
    *   **Output**: This will generate `pkg/state/api/v1/state_v1.pb.go` and `pkg/state/api/v1/state_v1_grpc.pb.go`.
    *   **Dependency**: Completion of Tasks 1.1, 1.2.

### 2. Implement In-Memory Store (`pkg/state/store`)

**Goal**: Create a thread-safe in-memory data store for bucket states.

*   **Task 2.1**: Define the `Store` interface in `pkg/state/store/store.go`.
    *   **Context**: Abstracting the store allows for future extensions (e.g., Redis).
    *   **Details**: Use the generated protobuf types for consistency.
        ```go
        package store
        import statepb "github.com/satmihir/fair/pkg/state/api/v1"
        type Store interface {
            ApplyDelta(seed, rowID, colID uint64, deltaProb float64, timestampMs uint64) (*statepb.Bucket, error)
            GetSeed(seed uint64) ([]*statepb.Bucket, error)
            EvictBefore(seed uint64) error
        }
        ```
*   **Task 2.2**: Implement `InMemoryStore` in `pkg/state/store/inmemory.go`.
    *   **Details**:
        *   Define `InMemoryStore` struct with a `sync.RWMutex` and a map `map[uint64]map[uint64]map[uint64]*statepb.Bucket`.
        *   Implement the `Store` interface methods (`ApplyDelta`, `GetSeed`, `EvictBefore`) with the aggregation logic (sum, clamp, max-timestamp) and locking as planned.
*   **Task 2.3**: Implement background eviction goroutine in `InMemoryStore`.
    *   **Details**:
        *   Add a `Start()` method to `InMemoryStore` that launches a goroutine.
        *   This goroutine periodically calculates the eviction seed based on `(current_seed - 3 * window_duration_in_seeds)` and calls `EvictBefore`. The `window_duration` will come from configuration.
        *   Implement a `Stop()` method that gracefully terminates the goroutine using a `context.Context`.
*   **Task 2.4**: Write unit tests for `InMemoryStore` in `pkg/state/store/inmemory_test.go`.
    *   **Context**: Validate aggregation logic, clamping, timestamp updates, concurrency, and eviction.
    *   **Dependency**: Completion of Tasks 2.1-2.3.

## Phase 2: Communication and Server Logic

### 3. Implement Broadcast Hub (`pkg/broadcast`)

**Goal**: Manage client connections and fan out aggregated bucket updates.

*   **Task 3.1**: Implement `Hub` and `Client` logic in `pkg/broadcast/hub.go`.
    *   **Context**: This component is the core of the real-time fan-out system.
    *   **Details**: Follow the original plan to create the `Hub` with `register`, `unregister`, `broadcast` channels, and the `Client` struct to manage each gRPC stream. Implement the `Run` method for the `Hub`'s main event loop.

*   **Task 3.2**: Write unit tests for `BroadcastHub` in `pkg/broadcast/hub_test.go`.
    *   **Context**: Verify client registration, unregistration, and message fan-out.

### 4. Implement gRPC Server (`pkg/state/service.go` & `cmd/server/main.go`)

**Goal**: Provide the network endpoint for the State Service.

*   **Task 4.1**: Create `StateServiceServer` implementation in `pkg/state/service.go`.
    *   **Details**: Implement the `statepb.StateServiceServer` interface. The `Service` struct will hold the `Store` and `broadcast.Hub` dependencies. Implement the `Sync` gRPC method as planned, handling `DeltaUpdate` and `StateRequest` messages.
*   **Task 4.2**: Update Server Configuration in `pkg/config/`.
    *   **Goal**: Make the server configurable via files or environment variables.
    *   **Details**:
        *   Modify the configuration struct in `pkg/config/types.go` to include new fields:
            *   `GRPCListenAddress` (e.g., `":50051"`)
            *   `MetricsListenAddress` (e.g., `":9090"`)
            *   `SeedWindowDuration` (e.g., `"5m"`)
            *   `SeedEvictionTTL` (e.g., `"15m"`, which is 3x the window duration).
            *   `BroadcastInterval` (e.g., `"250ms"`, for optional batching).
        *   Update the configuration loading logic to parse these values.
*   **Task 4.3**: Update `cmd/server/main.go` to run the gRPC service.
    *   **Details**:
        *   In `main()`, load the configuration.
        *   Initialize the `store.InMemoryStore` and `broadcast.Hub`.
        *   Start the hub and the store's eviction goroutine.
        *   Create the `state.Service` instance.
        *   Create a `grpc.NewServer()` and register the state service.
        *   Start listening on the configured `GRPCListenAddress`.
        *   Ensure graceful shutdown for the gRPC server, hub, and store.

*   **Task 4.4**: Update `Dockerfile` for gRPC port.
    *   **Details**: Add `EXPOSE 50051` (or the configured port) to the `Dockerfile` to make the gRPC service accessible.

## Phase 3: Client Integration and Operationalization

### 5. Implement State Service Client (`pkg/state/client`)

**Goal**: Create a reusable client to connect FAIR instances to the State Service.

*   **Task 5.1**: Implement the client logic in `pkg/state/client/client.go`.
    *   **Context**: This package will encapsulate all logic for connecting to, sending deltas to, and receiving updates from the State Service.
    *   **Details**:
        *   Create a `Client` struct that manages the gRPC connection and stream.
        *   Implement `Connect()`, `Close()`, and reconnection logic.
        *   Implement `SendDeltaUpdate()` which sends deltas asynchronously.
        *   Implement `RequestFullState()` for cold starts and seed rotations.
        *   The client should accept a callback function (e.g., `OnStateUpdate(bucket *statepb.Bucket)`) to be invoked when it receives broadcasts.
*   **Task 5.2**: Integrate State Service Client into `pkg/tracker`.
    *   **Context**: The core FAIR logic must be updated to use the new client.
    *   **Details**:
        *   Modify `pkg/tracker/tracker.go` to conditionally initialize the `state.Client` if a remote state service address is configured.
        *   Use the `OnStateUpdate` callback to apply "blind overwrites" to the tracker's local data structures.
        *   When local deltas are calculated, call the client's `SendDeltaUpdate()` method.
        *   Handle the logic for cold start and seed rotation by calling `RequestFullState()`.

### 6. Observability and Testing

*   **Task 6.1**: Instrument metrics in `pkg/state/service.go` and `pkg/broadcast/hub.go`.
    *   **Details**: Add Prometheus metrics for connected clients, deltas received, buckets broadcast, aggregation latency, and store size, as originally planned. Expose them on the configured `MetricsListenAddress`.
*   **Task 6.2**: Implement Integration Tests in `pkg/integration/state_service_test.go`.
    *   **Context**: End-to-end testing is critical for a distributed system.
    *   **Details**:
        *   Create a new test file for state service integration.
        *   Programmatically start a full State Service instance.
        *   Create multiple mock FAIR clients (`state.Client`) that connect to it.
        *   Write tests to verify:
            *   Eventual consistency: All clients receive the same state after multiple deltas are sent.
            *   Cold Start: A new client correctly fetches the full state.
            *   Seed Rotation: A client successfully requests and syncs a new seed's state.
            *   Disconnect/Reconnect: A client can disconnect, reconnect, and re-synchronize its state.

### 7. Documentation

*   **Task 7.1**: Update `README.md` and `DEPLOYMENT.md`.
    *   **Details**:
        *   Add a section on the State Service, its purpose, and architecture.
        *   Document all new configuration options for both the server and the FAIR library client.
        *   Update `k8s/` configuration files and document how to deploy the State Service.
        *   Explain how to enable distributed mode in FAIR instances by providing the service address.

---
**End of Plan**
---
