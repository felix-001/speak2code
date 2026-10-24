package mcp

import (
	"encoding/json"
	"fmt"
	"speak2code/internal/task"
)

type Caller func(taskID, request string)
type Session struct {
	Call       Caller
	ServerName string
}

func (s Session) Handle(data []byte) ([]byte, error) {
	req, err := Decode(data)
	if err != nil {
		return nil, err
	}
	switch req.Method {
	case "initialize":
		return ResponseMessage(req.ID, map[string]any{"protocolVersion": ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": s.ServerName, "version": "0.1.0"}})
	case "tools/list":
		return ResponseMessage(req.ID, map[string]any{"tools": []Tool{{Name: "self.code.request", Description: "请求端侧 Codex Agent 修改授权工作区并返回 diff", Type: 0, InputSchema: map[string]any{"type": "object", "properties": map[string]any{"request": map[string]string{"type": "string", "description": "编码需求"}}, "required": []string{"request"}}}}, "nextCursor": nil})
	case "tools/call":
		var params struct {
			Name      string `json:"name"`
			Arguments struct {
				Request string `json:"request"`
			} `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return ErrorMessage(req.ID, -32602, err.Error())
		}
		if params.Name != "self.code.request" || params.Arguments.Request == "" {
			return ErrorMessage(req.ID, -32602, "缺少有效编码需求")
		}
		taskID := fmt.Sprintf("lingxi-%v", req.ID)
		if s.Call != nil {
			s.Call(taskID, params.Arguments.Request)
		}
		return ResponseMessage(req.ID, map[string]any{"content": []map[string]string{{"type": "text", "text": "编码任务已接收，结果将通过事件回传"}}, "isError": false})
	default:
		return ErrorMessage(req.ID, -32601, "未知方法: "+req.Method)
	}
}

var _ = task.Event{}
