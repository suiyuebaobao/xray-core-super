package service

import (
	"context"
	"encoding/json"
	"strings"
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
	db, err := gorm.Open(sqlite.Open("file:relay_deploy_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.Node{},
		&model.Relay{},
		&model.RelayBackend{},
		&model.RelayConfigTask{},
	))
	return db
}

func TestRelayService_SaveBackendsRejectsDisabledExitNode(t *testing.T) {
	db := setupRelayDeployTestDB(t)
	ctx := context.Background()
	nodeRepo := repository.NewNodeRepository(db)
	relayRepo := repository.NewRelayRepository(db)
	backendRepo := repository.NewRelayBackendRepository(db)
	taskRepo := repository.NewRelayConfigTaskRepository(db)
	relaySvc := NewRelayService(relayRepo, backendRepo, taskRepo, nodeRepo, 3)

	disabledNode, err := nodeRepo.Create(ctx, &model.Node{
		Name:           "disabled-exit",
		Protocol:       "vless",
		Transport:      "tcp",
		Host:           "203.0.113.20",
		Port:           443,
		AgentBaseURL:   "http://203.0.113.20:8080",
		AgentTokenHash: "hash",
		IsEnabled:      false,
	})
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.Node{}).Where("id = ?", disabledNode.ID).Update("is_enabled", false).Error)
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

	_, err = relaySvc.SaveBackends(ctx, relay.ID, []model.RelayBackendRequest{{
		ExitNodeID: disabledNode.ID,
		ListenPort: 24443,
		IsEnabled:  true,
	}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "disabled")
}

func TestRelayService_CreateReloadTaskSkipsDisabledExitNodeBackends(t *testing.T) {
	db := setupRelayDeployTestDB(t)
	ctx := context.Background()
	nodeRepo := repository.NewNodeRepository(db)
	relayRepo := repository.NewRelayRepository(db)
	backendRepo := repository.NewRelayBackendRepository(db)
	taskRepo := repository.NewRelayConfigTaskRepository(db)
	relaySvc := NewRelayService(relayRepo, backendRepo, taskRepo, nodeRepo, 3)

	enabledNode, err := nodeRepo.Create(ctx, &model.Node{
		Name:           "enabled-exit",
		Protocol:       "vless",
		Transport:      "tcp",
		Host:           "203.0.113.10",
		Port:           443,
		AgentBaseURL:   "http://203.0.113.10:8080",
		AgentTokenHash: "hash",
		IsEnabled:      true,
	})
	require.NoError(t, err)
	disabledNode, err := nodeRepo.Create(ctx, &model.Node{
		Name:           "disabled-exit",
		Protocol:       "vless",
		Transport:      "tcp",
		Host:           "203.0.113.20",
		Port:           443,
		AgentBaseURL:   "http://203.0.113.20:8080",
		AgentTokenHash: "hash",
		IsEnabled:      false,
	})
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.Node{}).Where("id = ?", disabledNode.ID).Update("is_enabled", false).Error)
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
	require.NoError(t, db.Create(&model.RelayBackend{
		RelayID: relay.ID, ExitNodeID: enabledNode.ID, Name: "enabled-backend",
		ListenPort: 24443, TargetHost: enabledNode.Host, TargetPort: enabledNode.Port,
		IsEnabled: true,
	}).Error)
	require.NoError(t, db.Create(&model.RelayBackend{
		RelayID: relay.ID, ExitNodeID: disabledNode.ID, Name: "disabled-backend",
		ListenPort: 24444, TargetHost: disabledNode.Host, TargetPort: disabledNode.Port,
		IsEnabled: true,
	}).Error)

	require.NoError(t, relaySvc.CreateReloadTask(ctx, relay.ID))

	var task model.RelayConfigTask
	require.NoError(t, db.Where("relay_id = ? AND action = ?", relay.ID, "RELOAD_CONFIG").First(&task).Error)
	require.NotNil(t, task.Payload)
	require.NotContains(t, *task.Payload, "disabled-backend")
	require.NotContains(t, *task.Payload, "203.0.113.20")

	var payload RelayReloadPayload
	require.NoError(t, json.Unmarshal([]byte(*task.Payload), &payload))
	require.Len(t, payload.Backends, 1)
	require.Equal(t, "enabled-backend", payload.Backends[0].Name)
	require.False(t, strings.Contains(*task.Payload, "24444"))
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
