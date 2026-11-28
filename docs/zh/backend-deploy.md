# 部署后端服务

生产环境的账号、云存档和自托管排行榜使用 Postgres。SQLite 适合开发、测试和带持久卷的单节点部署，内存存储仅用于测试。

让 `accountsd`、`cloudsaved` 和 `leaderboardd` 位于 TLS 终止代理之后，并配置限流与健康检查。通过受保护的密钥存储共享 `accountsd genkey` 生成的密钥集。

使用托管 Postgres 快照或 `pg_dump` 并演练恢复，滚动发布前运行 schema 迁移。遇到更新的 schema 版本时拒绝旧二进制并向前升级。文件排行榜在接入共享 SQL 存储前只适合单进程，SQLite 备份需包含 WAL 文件。
