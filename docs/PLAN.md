# 智选购 MVP 执行计划

## Goal

在个人可控的本地环境中，交付一条能被真实演示和追问的 AI 电商导购主链路：自然语言需求经受控 Agent 与 RAG 支持生成推荐，后端校验交易事实后进入购物车、订单和余额支付，并可从运营端追踪 Agent 与知识库处理过程。

## Current Behavior

当前仓库已完成 M1.1 工程 bootstrap，以及 M1.2 的商品读链路和用户认证读写链路：Go workspace、Gateway/五个服务启动骨架、Docker Compose 依赖栈、五个逻辑 schema、catalog seed、商品 gRPC/HTTP 读接口和 Redis Cache Aside 均已建立；`user-service` 已提供注册、登录、JWT、个人画像及地址归属校验，Gateway 已提供对应的 HTTP API。M1.2 的延迟二次缓存删除与失败重试、交易、RAG 和 Agent 仍待后续里程碑开发。详见 [智选购 AI 导购 MVP 设计文档](智选购-ai导购-mvp-design.md)。

## Proposed Solution

按 [TASKS.md](TASKS.md) 的 M1-M6 依赖顺序建设：先让商品读路径和本地依赖可用，再完成本地强一致交易，随后建立可重放的 RAG 事件链和受控 Agent，最后接入页面、运营排障和端到端验证。

任何实现都以以下边界为前提：MySQL 是交易事实源；Redis 仅承担读缓存和短期状态；Kafka 承担知识库和行为/评价事件，不承担缓存删除；LLM 仅输出候选 SKU 与理由，业务 Tool 负责最终校验。

## Risks

| 风险 | 影响 | 应对 |
| --- | --- | --- |
| 模型或 Eino API 差异 | Agent 实现受 Provider 变化影响 | 通过 Provider/Tool 适配层隔离，先固定结构化输出契约 |
| 本地 Milvus/Kafka 运维复杂 | 演示环境不可复现 | Docker Compose 健康检查、种子数据和独立依赖验证 |
| 缓存与交易语义被混淆 | 出现脏读、超卖或重复扣款 | 支付使用 MySQL transaction 与条件更新，缓存仅做读优化 |
| Kafka 至少一次投递 | 产生重复 Chunk 或统计 | Outbox、`event_id`、消费幂等表、retry/dead-letter |
| 简历性能数据不可证实 | 面试追问无法自洽 | 未实测不写数值，压测结果必须记录完整环境与命令 |

## Validation Strategy

1. 每个服务能力先以 table-driven unit tests 覆盖正常、边界和失败分支。
2. 对交易、RAG、Kafka、缓存和 Agent 运行本地依赖集成测试。
3. 对完整用户路径执行浏览器/API 演示：导购、推荐校验、建单、支付、资料上传、RAG 问答和 Agent 回放。
4. 完成前运行 `gofmt`、`go test`、`go vet`、前端 lint/typecheck，并记录实际命令和结果。

## Milestone Gate

进入下一里程碑前，当前里程碑必须可独立启动、可重复验证、没有架构边界偏移，并同步更新相关文档。Git 初始化后，每个已验证里程碑形成单独 commit；未获用户明确授权不推送远程仓库。
