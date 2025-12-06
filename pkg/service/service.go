package service

import (
	"context"
	"fmt"

	"github.com/satmihir/fair/pkg/request"
	"github.com/satmihir/fair/pkg/tracker"
)

var (
	// ErrInvalidOutcome indicates that the provided outcome string is not valid.
	ErrInvalidOutcome = fmt.Errorf("invalid outcome. Must be 'success' or 'failure'")
)

// RegisterRequest contains the parameters for registering a request.
type RegisterRequest struct {
	ClientID string `json:"client_id"`
}

// RegisterResponse contains the decision and stats.
type RegisterResponse struct {
	ShouldThrottle bool                 `json:"should_throttle"`
	Stats          *request.ResultStats `json:"stats,omitempty"`
}

// ReportRequest contains the outcome to report.
type ReportRequest struct {
	ClientID string `json:"client_id"`
	Outcome  string `json:"outcome"`
}

// Service defines the interface for the fairness service.
type Service interface {
	Register(ctx context.Context, req RegisterRequest) (*RegisterResponse, error)
	Report(ctx context.Context, req ReportRequest) error
	Close()
}

type fairService struct {
	tracker *tracker.FairnessTracker
}

// NewService creates a new Service instance using the provided tracker.
func NewService(t *tracker.FairnessTracker) Service {
	return &fairService{
		tracker: t,
	}
}

func (s *fairService) Register(ctx context.Context, req RegisterRequest) (*RegisterResponse, error) {
	result := s.tracker.RegisterRequest(ctx, []byte(req.ClientID))
	return &RegisterResponse{
		ShouldThrottle: result.ShouldThrottle,
		Stats:          result.ResultStats,
	}, nil
}

func (s *fairService) Report(ctx context.Context, req ReportRequest) error {
	var outcome request.Outcome
	switch req.Outcome {
	case "success":
		outcome = request.OutcomeSuccess
	case "failure":
		outcome = request.OutcomeFailure
	default:
		return ErrInvalidOutcome
	}

	s.tracker.ReportOutcome(ctx, []byte(req.ClientID), outcome)
	return nil
}

func (s *fairService) Close() {
	s.tracker.Close()
}
