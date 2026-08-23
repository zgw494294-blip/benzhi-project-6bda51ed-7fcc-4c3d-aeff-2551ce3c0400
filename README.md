# 博物馆展签事实核校与发布系统

本项目为博物馆策展团队提供展签内容治理服务，将事实主张、来源证据、专业核校、文字复核、版本冻结和发布签章串成可追溯流程。

标准命令：

```text
go build ./...
go test ./...
go run ./cmd/server -addr=127.0.0.1:19081
go run ./cmd/server -addr=127.0.0.1:19081 -selfcheck
```

服务默认监听 `127.0.0.1:19081`，可通过 `-addr=127.0.0.1:<port>` 或 `PORT` 环境变量配置。数据默认保存在当前目录的 `.benzhi/ledger.json`，可通过 `-data` 指定。

主要接口：

- `POST /api/v1/dossiers` 创建草稿案卷，`GET /api/v1/dossiers` 使用 `status`、`exhibitionName`、`owner`、`limit` 和 `cursor` 组合检索。
- `GET/PATCH /api/v1/dossiers/{id}` 查询案卷审计或更正草稿资料及交接负责人。
- `POST /api/v1/dossiers/{id}/evidence` 登记证据，`POST /api/v1/dossiers/{id}/claims` 原子创建主张及关联；`PUT /api/v1/dossiers/{id}/claims/{claimId}/evidence` 替换关联集合。
- `POST /api/v1/dossiers/{id}/evidence/{evidenceId}/supersede` 作废并迁移当前引用，`GET /api/v1/dossiers/{id}/evidence/{evidenceId}/usage` 查询各修订使用情况。
- `POST /api/v1/dossiers/{id}/precheck` 执行预检；`GET /api/v1/dossiers/{id}/prechecks` 查询历史，`GET /api/v1/dossiers/{id}/prechecks/diff?fromVersion=2&toVersion=5` 比较问题变化。
- `PUT /api/v1/dossiers/{id}/expert-review/drafts/{claimId}` 暂存逐项结论，`GET /api/v1/dossiers/{id}/expert-review` 查询进度，`POST /api/v1/dossiers/{id}/expert-review/finalize` 统一提交；原批量提交入口保持兼容。
- `POST /api/v1/dossiers/{id}/copy-review` 提交文字建议，`POST /api/v1/dossiers/{id}/revise` 派生修订，`GET /api/v1/dossiers/{id}/revisions/diff?from=1&to=2` 查询差异。
- `GET /api/v1/dossiers/{id}/copy-suggestions` 查询建议，`PATCH /api/v1/dossiers/{id}/copy-suggestions/{suggestionId}` 记录采纳或驳回处置。
- `POST /api/v1/dossiers/{id}/freeze` 冻结确定性快照，`POST /api/v1/dossiers/{id}/issue` 幂等签发凭据，`GET /api/v1/credentials/{credentialNo}` 查询凭据、快照和审计轨迹，`GET /api/v1/credentials/{credentialNo}/verify` 独立验真。

所有写请求通过请求体 `expectedVersion` 或请求头 `X-Expected-Version` 执行乐观并发控制。核校与签发请求使用 `X-Operator` 记录操作者；预检、批量核校和签发可使用 `Idempotency-Key` 安全重放。
