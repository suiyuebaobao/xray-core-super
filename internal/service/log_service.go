package service

import (
	"context"
	"fmt"
	"time"

	"suiyue/internal/model"
	"suiyue/internal/repository"
)

type ClientLogContext struct {
	UserID       *uint64
	Username     *string
	ClientIP     *string
	ForwardedFor *string
	RealIP       *string
	UserAgent    *string
}

type OperationLogService struct {
	repo *repository.OperationLogRepository
}

func NewOperationLogService(repo *repository.OperationLogRepository) *OperationLogService {
	return &OperationLogService{repo: repo}
}

func (s *OperationLogService) Record(ctx context.Context, logCtx ClientLogContext, actorType, action, result, summary string, targetType *string, targetID *uint64, extra interface{}) error {
	if s == nil || s.repo == nil {
		return nil
	}
	extraJSON, err := repository.MustJSONString(extra)
	if err != nil {
		return err
	}
	return s.repo.Create(ctx, &model.OperationLog{
		ActorType:     actorType,
		ActorUserID:   logCtx.UserID,
		ActorUsername: logCtx.Username,
		ClientIP:      logCtx.ClientIP,
		ForwardedFor:  logCtx.ForwardedFor,
		RealIP:        logCtx.RealIP,
		UserAgent:     logCtx.UserAgent,
		Action:        action,
		TargetType:    targetType,
		TargetID:      targetID,
		Result:        result,
		Summary:       summary,
		ExtraJSON:     extraJSON,
	})
}

func (s *OperationLogService) List(ctx context.Context, page, size int, actorType, action, keyword string) ([]model.OperationLog, int64, error) {
	if s == nil || s.repo == nil {
		return []model.OperationLog{}, 0, nil
	}
	return s.repo.List(ctx, page, size, actorType, action, keyword)
}

type DeploymentLogService struct {
	repo *repository.DeploymentLogRepository
}

func NewDeploymentLogService(repo *repository.DeploymentLogRepository) *DeploymentLogService {
	return &DeploymentLogService{repo: repo}
}

func (s *DeploymentLogService) Record(ctx context.Context, logCtx ClientLogContext, deployType, targetServerIP, targetRole, result string, requestSummary interface{}, duration time.Duration, nodeID *uint64, nodeIDs []uint64, nodeHostID *uint64, relayID *uint64, backendIDs []uint64, steps []Step, errDetail *string) error {
	if s == nil || s.repo == nil {
		return nil
	}
	requestJSON, err := repository.MustJSONString(requestSummary)
	if err != nil {
		return err
	}
	nodeIDsJSON, err := repository.MustJSONString(nodeIDs)
	if err != nil {
		return err
	}
	backendIDsJSON, err := repository.MustJSONString(backendIDs)
	if err != nil {
		return err
	}
	var durationMS *uint64
	if duration > 0 {
		v := uint64(duration.Milliseconds())
		durationMS = &v
	}
	stepRows := make([]model.DeploymentLogStep, 0, len(steps))
	for _, step := range steps {
		stepRows = append(stepRows, model.DeploymentLogStep{
			Name:    step.Name,
			Status:  step.Status,
			Message: step.Message,
		})
	}
	return s.repo.CreateWithSteps(ctx, &model.DeploymentLog{
		OperatorUserID:   logCtx.UserID,
		OperatorUsername: logCtx.Username,
		OperatorIP:       logCtx.ClientIP,
		DeployType:       deployType,
		TargetServerIP:   targetServerIP,
		TargetRole:       targetRole,
		RequestSummary:   requestJSON,
		Result:           result,
		DurationMS:       durationMS,
		NodeID:           nodeID,
		NodeIDs:          nodeIDsJSON,
		NodeHostID:       nodeHostID,
		RelayID:          relayID,
		BackendIDs:       backendIDsJSON,
		ErrorDetail:      errDetail,
	}, stepRows)
}

func (s *DeploymentLogService) List(ctx context.Context, page, size int, deployType, result, keyword string) ([]model.DeploymentLog, int64, error) {
	if s == nil || s.repo == nil {
		return []model.DeploymentLog{}, 0, nil
	}
	return s.repo.List(ctx, page, size, deployType, result, keyword)
}

type RuntimeLogService struct {
	reader *repository.RuntimeLogReader
}

func NewRuntimeLogService(reader *repository.RuntimeLogReader) *RuntimeLogService {
	return &RuntimeLogService{reader: reader}
}

func (s *RuntimeLogService) Read(ctx context.Context, source string, lineCount int, keyword string) ([]repository.RuntimeLogLine, error) {
	if s == nil || s.reader == nil {
		return nil, fmt.Errorf("runtime log reader is not configured")
	}
	return s.reader.Read(repository.RuntimeLogSource(source), lineCount, keyword)
}
