package service

import (
	"context"
	"testing"
	"time"

	"suiyue/internal/model"
	"suiyue/internal/repository"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupRelayDeployTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Node{},
		&model.Relay{},
		&model.RelayBackend{},
		&model.RelayConfigTask{},
	))
	require.NoError(t, db.Exec("CREATE TABLE IF NOT EXISTS relay_config_tasks (id INTEGER PRIMARY KEY AUTOINCREMENT, relay_id INTEGER, action TEXT, payload TEXT, status TEXT, retry_count INTEGER, last_error TEXT, scheduled_at DATETIME, locked_at DATETIME, lock_token TEXT, executed_at DATETIME, idempotency_key TEXT)").Error)
	return db
}

func TestFinalizeRelayDeploy_BindsBackendWithExitNodePort(t *testing.T) {
	db := setupRelayDeployTestDB(t)
	ctx := context.Background()
	nodeRepo := repository.NewNodeRepository(db)
	relayRepo := repository.NewRelayRepository(db)
	backendRepo := repository.NewRelayBackendRepository(db)
	taskRepo := repository.NewRelayConfigTaskRepository(db)
	relaySvc := NewRelayService(relayRepo, backendRepo, taskRepo, nodeRepo, 3)
	deploySvc := NewRelayDeployServiceWithAutomation(relayRepo, relaySvc, nodeRepo, nil)

	exitNode, err := nodeRepo.Create(ctx, &model.Node{
		Name:           "exit",
		Protocol:       "vless",
		Transport:      "tcp",
		Host:           "203.0.113.10",
		Port:           443,
		AgentBaseURL:   "http://203.0.113.10:8080",
		AgentTokenHash: "hash",
		IsEnabled:      true,
	})
	require.NoError(t, err)
	relay, err := relayRepo.Create(ctx, &model.Relay{
		Name:           "relay",
		Host:           "198.51.100.10",
		ForwarderType:  "haproxy",
		AgentBaseURL:   "http://198.51.100.10:8080",
		AgentTokenHash: "hash",
		Status:         "online",
		IsEnabled:      true,
	})
	require.NoError(t, err)

	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			var task model.RelayConfigTask
			if err := db.Where("relay_id = ? AND action = ?", relay.ID, "RELOAD_CONFIG").Order("id DESC").First(&task).Error; err == nil {
				_ = taskRepo.MarkDone(context.Background(), task.ID, time.Now())
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	backendIDs, err := deploySvc.finalizeRelayDeploy(ctx, &RelayDeployRequest{
		SSHHost:    "198.51.100.10",
		ExitNodeID: exitNode.ID,
		ListenPort: 24443,
	}, relay.ID, func(string, string, string) {})

	require.NoError(t, err)
	require.Len(t, backendIDs, 1)
	backends, err := backendRepo.ListByRelayID(ctx, relay.ID)
	require.NoError(t, err)
	require.Len(t, backends, 1)
	require.Equal(t, exitNode.ID, backends[0].ExitNodeID)
	require.EqualValues(t, 24443, backends[0].ListenPort)
	require.Equal(t, "203.0.113.10", backends[0].TargetHost)
	require.EqualValues(t, 443, backends[0].TargetPort)
}
