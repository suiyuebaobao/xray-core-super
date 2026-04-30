// cmd/node-agent-mock/main.go — 节点代理 Mock 服务。
//
// 功能：
// - 模拟 node-agent 的最小可用版本
// - 提供心跳上报接口（POST /api/heartbeat）
// - 提供流量计数器查询接口（GET /api/traffic）
// - 提供任务执行接口（POST /api/tasks/execute）
//
// 用途：
// - 在真实 node-agent 开发完成前，让中心服务可以先行开发和测试
// - 返回模拟数据，模拟节点在线状态和流量统计
//
// 启动方式：
//
//	go run ./cmd/node-agent-mock
//
// 环境变量：
//
//	AGENT_MOCK_PORT=8080  （默认 8080）
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// TrafficItem 模拟的单个用户流量计数器。
type TrafficItem struct {
	XrayUserKey string `json:"xray_user_key"`
	UplinkTotal   uint64 `json:"uplink_total"`
	DownlinkTotal uint64 `json:"downlink_total"`
}

// HeartbeatRequest 心跳请求体。
type HeartbeatRequest struct {
	NodeID   int64  `json:"node_id"`
	Version  string `json:"version"`
	Token    string `json:"token"`
}

// HeartbeatResponse 心跳响应体。包含待执行的任务列表。
type HeartbeatResponse struct {
	Success bool    `json:"success"`
	Tasks   []Task  `json:"tasks"`
}

// Task 待执行任务。
type Task struct {
	ID              int64  `json:"id"`
	Action          string `json:"action"`
	Payload         string `json:"payload"`
	IdempotencyKey  string `json:"idempotency_key"`
}

// TaskResult 任务执行结果上报。
type TaskResult struct {
	TaskID      int64  `json:"task_id"`
	Success     bool   `json:"success"`
	Error       string `json:"error,omitempty"`
}

// mockStore 模拟数据存储。
type mockStore struct {
	mu       sync.RWMutex
	traffic  map[string]map[string]*TrafficItem // nodeID -> xrayUserKey -> traffic
	tasks    []Task
	taskID   int64
}

var store = &mockStore{
	traffic: make(map[string]map[string]*TrafficItem),
	tasks:   []Task{},
	taskID:  0,
}

func main() {
	port := getEnvInt("AGENT_MOCK_PORT", 8080)

	mux := http.NewServeMux()

	// 心跳上报：服务端返回待执行任务列表
	mux.HandleFunc("/api/heartbeat", handleHeartbeat)

	// 流量计数器查询
	mux.HandleFunc("/api/traffic", handleTraffic)

	// 任务执行结果上报
	mux.HandleFunc("/api/tasks/result", handleTaskResult)

	// 注入测试任务（仅开发用）
	mux.HandleFunc("/api/tasks/seed", handleSeedTasks)

	addr := ":" + strconv.Itoa(port)
	log.Printf("[node-agent-mock] starting on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("[node-agent-mock] server failed: %v", err)
	}
}

// handleHeartbeat 处理心跳上报。
// 请求 POST /api/heartbeat {"node_id": 1, "version": "0.1.0", "token": "..."}
// 响应 {"success": true, "tasks": [...]}
func handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "message": "method not allowed"})
		return
	}

	var req HeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "message": "invalid request"})
		return
	}

	log.Printf("[node-agent-mock] heartbeat from node %d", req.NodeID)

	// 返回当前待执行的任务
	store.mu.RLock()
	tasks := store.tasks
	store.mu.RUnlock()

	writeJSON(w, http.StatusOK, HeartbeatResponse{
		Success: true,
		Tasks:   tasks,
	})
}

// handleTraffic 返回模拟流量数据。
// GET /api/traffic?node_id=1
func handleTraffic(w http.ResponseWriter, r *http.Request) {
	nodeID := r.URL.Query().Get("node_id")
	if nodeID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "message": "missing node_id"})
		return
	}

	store.mu.RLock()
	defer store.mu.RUnlock()

	traffic := store.traffic[nodeID]
	if traffic == nil {
		traffic = make(map[string]*TrafficItem)
	}

	items := make([]TrafficItem, 0, len(traffic))
	for _, item := range traffic {
		items = append(items, *item)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    items,
	})
}

// handleTaskResult 处理任务执行结果上报。
// POST /api/tasks/result {"task_id": 1, "success": true, "error": ""}
func handleTaskResult(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "message": "method not allowed"})
		return
	}

	var result TaskResult
	if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "message": "invalid request"})
		return
	}

	log.Printf("[node-agent-mock] task %d result: success=%v error=%s", result.TaskID, result.Success, result.Error)

	// 从任务列表中移除已完成的任务
	store.mu.Lock()
	newTasks := make([]Task, 0, len(store.tasks))
	for _, t := range store.tasks {
		if t.ID != result.TaskID {
			newTasks = append(newTasks, t)
		}
	}
	store.tasks = newTasks
	store.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// handleSeedTasks 注入测试任务（开发调试用）。
func handleSeedTasks(w http.ResponseWriter, r *http.Request) {
	store.mu.Lock()
	defer store.mu.Unlock()

	store.taskID++
	store.tasks = append(store.tasks, Task{
		ID:             store.taskID,
		Action:         "UPSERT_USER",
		Payload:        `{"xray_user_key":"test@suiyue.local","uuid":"test-uuid"}`,
		IdempotencyKey: fmt.Sprintf("task-%d-%d", store.taskID, time.Now().Unix()),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("seeded task id=%d", store.taskID),
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func getEnvInt(key string, defaultVal int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return n
}
