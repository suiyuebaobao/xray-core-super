package service_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"suiyue/internal/model"
	"suiyue/internal/repository"
	"suiyue/internal/service"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupLogServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.OperationLog{},
		&model.DeploymentLog{},
		&model.DeploymentLogStep{},
	))
	return db
}

func ptrString(value string) *string {
	return &value
}

func ptrUint64(value uint64) *uint64 {
	return &value
}

func TestOperationLogService_Record_PersistsClientIP(t *testing.T) {
	db := setupLogServiceTestDB(t)
	svc := service.NewOperationLogService(repository.NewOperationLogRepository(db))

	err := svc.Record(context.Background(), service.ClientLogContext{
		UserID:       ptrUint64(7),
		Username:     ptrString("alice"),
		ClientIP:     ptrString("203.0.113.10"),
		ForwardedFor: ptrString("198.51.100.1, 203.0.113.10"),
		RealIP:       ptrString("203.0.113.10"),
		UserAgent:    ptrString("Playwright"),
	}, "user", "login", "success", "用户登录成功", ptrString("user"), ptrUint64(7), map[string]interface{}{"source": "test"})
	require.NoError(t, err)

	logs, total, err := svc.List(context.Background(), 1, 20, "user", "login", "203.0.113.10")
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, logs, 1)
	require.Equal(t, "203.0.113.10", *logs[0].ClientIP)
	require.Equal(t, "198.51.100.1, 203.0.113.10", *logs[0].ForwardedFor)
	require.Contains(t, *logs[0].ExtraJSON, "source")
}

func TestDeploymentLogService_Record_PersistsSteps(t *testing.T) {
	db := setupLogServiceTestDB(t)
	svc := service.NewDeploymentLogService(repository.NewDeploymentLogRepository(db))

	err := svc.Record(context.Background(), service.ClientLogContext{
		UserID:   ptrUint64(1),
		Username: ptrString("admin"),
		ClientIP: ptrString("127.0.0.1"),
	}, "exit_deploy", "156.238.231.16", "exit", "success", map[string]interface{}{"transport": []string{"tcp", "xhttp"}}, 1500*time.Millisecond, ptrUint64(105), []uint64{105, 106}, ptrUint64(9), nil, nil, []service.Step{
		{Name: "create_records", Status: "success", Message: "created logical nodes"},
		{Name: "sync_users", Status: "success", Message: "queued active users"},
	}, nil)
	require.NoError(t, err)

	logs, total, err := svc.List(context.Background(), 1, 20, "exit_deploy", "success", "156.238.231.16")
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, logs, 1)
	require.Len(t, logs[0].Steps, 2)
	require.Equal(t, "create_records", logs[0].Steps[0].Name)
	require.Equal(t, "sync_users", logs[0].Steps[1].Name)
	require.Equal(t, uint64(1500), *logs[0].DurationMS)
	require.Contains(t, *logs[0].NodeIDs, "105")
}

func TestRuntimeLogService_Read_FiltersKeyword(t *testing.T) {
	logDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(logDir, "api.log"), []byte("info boot\nerror failed deploy\nwarn slow request\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(logDir, "worker.log"), []byte("info worker boot\n"), 0o600))

	svc := service.NewRuntimeLogService(repository.NewRuntimeLogReader(logDir))
	lines, err := svc.Read(context.Background(), "api", 100, "failed")
	require.NoError(t, err)
	require.Len(t, lines, 1)
	require.Equal(t, "error", lines[0].Level)
	require.Contains(t, lines[0].Message, "failed deploy")
}

func TestRuntimeLogService_Read_MissingFileReturnsEmpty(t *testing.T) {
	logDir := t.TempDir()
	svc := service.NewRuntimeLogService(repository.NewRuntimeLogReader(logDir))

	lines, err := svc.Read(context.Background(), "api", 100, "")
	require.NoError(t, err)
	require.Empty(t, lines)
}
