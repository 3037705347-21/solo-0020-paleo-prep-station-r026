# 古生物修复制备台

古生物修复制备台是一个使用 Go 编写的后端基线，用于记录化石标本、审批修复方案、回填制备进度，并在全部工序完成后办理研究借阅放行。

该服务只使用进程内仓储，便于在本机重复启动和检查。它不依赖数据库、远端接口或其他在线资源。

## 目录

- `cmd/paleo-prep-station`：HTTP 服务入口和监听参数。
- `cmd/workflowcheck`：通过真实 HTTP 处理器执行四条业务链路的本地验证命令。
- `internal/httpapi`：路由、请求解码、状态码映射和 JSON 输出。
- `internal/station`：跨领域对象的编排、事务边界和业务流程。
- `internal/domain`：标本、方案、进度和放行记录的状态与不变量。
- `internal/repository`：互斥保护的内存仓储和原子提交。
- `internal/contracts`：稳定错误模型和公共请求结构。

## 构建与运行

```bash
go build ./...
go run ./cmd/paleo-prep-station --addr 127.0.0.1:18133
```

服务启动后，`GET /healthz` 返回就绪状态。

## 主要接口

- `GET /specimens` 与 `POST /specimens`
- `GET /specimens/{id}`
- `PATCH /specimens/{id}`：更正未完成修复标本的分类提示、基质类型或夹克备注
- `GET /specimens/{id}/amendments`：查阅标本资料更正历史
- `POST /specimens/{id}/plans`
- `GET /plans/{id}`
- `POST /plans/{id}/approval`
- `POST /plans/{id}/reevaluation`：确认资料更正后草稿方案的方法与目标仍然成立
- `POST /plans/{id}/progress`
- `POST /specimens/{id}/release`

### 标本资料更正

标本尚未完成修复时，可通过 `PATCH /specimens/{id}` 带原因更正资料：

```json
{
  "matrix_kind": "泥岩",
  "reason": "岩性薄片复核，基质由粉砂岩改为泥岩",
  "expected_version": 4,
  "request_id": "amend-2026-0005"
}
```

- `expected_version` 必填，必须与当前标本版本一致，否则返回 `409 specimen_version_conflict`。
- 至少一个字段发生真实变化；无变化返回 `409 specimen_unchanged`，不递增版本。
- 基质类型或分类提示变化影响方案方法与目标：草稿方案会被置为待重新评估（审批前必须调用 `POST /plans/{id}/reevaluation`）；已审批或进行中的方案直接返回 `409 plan_references_specimen_data`。夹克备注不影响方案。
- `stabilized` 或 `available` 标本返回 `409 specimen_not_amendable`。
- 重复提交携带相同 `request_id` 会确定性重放首次结果；同一凭证用于不同内容返回 `409 idempotency_key_reused`。

## 本地验证

每条业务链路都可以独立执行：

```bash
go run ./cmd/workflowcheck --workflow register-specimen
go run ./cmd/workflowcheck --workflow amend-specimen
go run ./cmd/workflowcheck --workflow approve-prep-plan
go run ./cmd/workflowcheck --workflow log-preparation
go run ./cmd/workflowcheck --workflow release-for-study
```

这些检查会启动临时 HTTP 处理器并调用真实公共路由。

## 环境变量

项目不需要环境变量，也没有外部服务依赖。

## 测试边界

本基线按要求暂不生成单元测试、测试夹具或端到端测试文件。后续任务阶段会补充红绿验证测试，届时再替换或扩展当前的生产级链路检查。
