package studio

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

const (
	hostMethodTasksCreate   = "tasks.create"
	hostMethodTasksGet      = "tasks.get"
	hostMethodTasksList     = "tasks.list"
	hostMethodTasksDelete   = "tasks.delete"
	hostMethodPlatformsList = "platforms.list"
	hostMethodModelsList    = "models.list"
	hostMethodUsersGet      = "users.get"
)

func hostInvoke(ctx context.Context, host sdk.Host, method string, payload map[string]interface{}) (map[string]interface{}, error) {
	if host == nil {
		return nil, fmt.Errorf("host 未启用")
	}
	resp, err := host.Invoke(ctx, sdk.HostInvokeRequest{
		Method:  method,
		Payload: payload,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return map[string]interface{}{}, nil
	}
	if strings.EqualFold(resp.Status, "error") {
		if msg, _ := resp.Payload["message"].(string); msg != "" {
			return nil, fmt.Errorf("%s", msg)
		}
		return nil, fmt.Errorf("host method %s 返回错误", method)
	}
	return resp.Payload, nil
}

type hostTask struct {
	ID           int64                  `json:"id"`
	TaskType     string                 `json:"task_type"`
	Status       string                 `json:"status"`
	Progress     int                    `json:"progress"`
	Input        map[string]interface{} `json:"input"`
	Output       map[string]interface{} `json:"output"`
	Attributes   map[string]interface{} `json:"attributes"`
	ErrorMessage string                 `json:"error_message"`
	CreatedAt    string                 `json:"created_at"`
	UpdatedAt    string                 `json:"updated_at"`
	CompletedAt  string                 `json:"completed_at,omitempty"`
}

func hostCreateTask(ctx context.Context, host sdk.Host, pluginID, taskType string, userID int64, input map[string]interface{}, attributes map[string]interface{}) (*hostTask, error) {
	payload := map[string]interface{}{
		"plugin_id":    pluginID,
		"task_type":    taskType,
		"user_id":      userID,
		"input":        input,
		"priority":     0,
		"max_attempts": 3,
	}
	if len(attributes) > 0 {
		payload["attributes"] = attributes
	}
	resp, err := hostInvoke(ctx, host, hostMethodTasksCreate, payload)
	if err != nil {
		return nil, err
	}
	return hostTaskFromPayload(firstValue(resp, "task", "data", "result", ""))
}

func hostGetTask(ctx context.Context, host sdk.Host, pluginID string, userID, taskID int64) (*hostTask, error) {
	payload := map[string]interface{}{
		"task_id": taskID,
		"user_id": userID,
	}
	if pluginID != "" {
		payload["plugin_id"] = pluginID
	}
	resp, err := hostInvoke(ctx, host, hostMethodTasksGet, payload)
	if err != nil {
		return nil, err
	}
	return hostTaskFromPayload(firstValue(resp, "task", "data", "result", ""))
}

type hostTaskListResponse struct {
	Tasks []*hostTask
	Total int
}

func hostListTasks(ctx context.Context, host sdk.Host, pluginID string, userID int64, taskType, status string, limit, offset int) (*hostTaskListResponse, error) {
	payload := map[string]interface{}{
		"user_id":   userID,
		"task_type": taskType,
		"status":    status,
		"limit":     limit,
		"offset":    offset,
	}
	if pluginID != "" {
		payload["plugin_id"] = pluginID
	}
	resp, err := hostInvoke(ctx, host, hostMethodTasksList, payload)
	if err != nil {
		return nil, err
	}
	out := &hostTaskListResponse{Total: intFromAny(firstValue(resp, "total", "count"))}
	if tasks, ok := firstValue(resp, "tasks", "items", "data").([]interface{}); ok {
		for _, item := range tasks {
			task, err := hostTaskFromPayload(item)
			if err != nil {
				return nil, err
			}
			out.Tasks = append(out.Tasks, task)
		}
	}
	if out.Total == 0 {
		out.Total = len(out.Tasks)
	}
	return out, nil
}

func hostDeleteTask(ctx context.Context, host sdk.Host, pluginID string, userID, taskID int64) error {
	payload := map[string]interface{}{
		"task_id": taskID,
		"user_id": userID,
	}
	if pluginID != "" {
		payload["plugin_id"] = pluginID
	}
	_, err := hostInvoke(ctx, host, hostMethodTasksDelete, payload)
	return err
}

func hostListPlatforms(ctx context.Context, host sdk.Host) ([]interface{}, error) {
	resp, err := hostInvoke(ctx, host, hostMethodPlatformsList, map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	if items, ok := firstValue(resp, "platforms", "items", "data").([]interface{}); ok {
		return items, nil
	}
	return nil, nil
}

func hostListModels(ctx context.Context, host sdk.Host, platform, capability string) ([]interface{}, error) {
	payload := map[string]interface{}{}
	if platform != "" {
		payload["platform"] = platform
	}
	if capability != "" {
		payload["capability"] = capability
	}
	resp, err := hostInvoke(ctx, host, hostMethodModelsList, payload)
	if err != nil {
		return nil, err
	}
	if items, ok := firstValue(resp, "models", "items", "data").([]interface{}); ok {
		return items, nil
	}
	return nil, nil
}

func hostTaskFromPayload(value interface{}) (*hostTask, error) {
	if value == nil {
		return nil, fmt.Errorf("task payload is nil")
	}
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var task hostTask
	if err := json.Unmarshal(body, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func firstValue(payload map[string]interface{}, keys ...string) interface{} {
	if payload == nil {
		return nil
	}
	for _, key := range keys {
		if key == "" {
			return payload
		}
		if value, ok := payload[key]; ok {
			return value
		}
	}
	return nil
}

func intFromAny(value interface{}) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return 0
	}
}
